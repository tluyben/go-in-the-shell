package aprocess

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/term"
)

type Screen struct {
	content [][]rune
	width   int
	height  int
	cursorX int
	cursorY int
	mu      sync.Mutex
}

func NewScreen(width, height int) *Screen {
	content := make([][]rune, height)
	for i := range content {
		content[i] = make([]rune, width)
		for j := range content[i] {
			content[i][j] = ' '
		}
	}
	return &Screen{content: content, width: width, height: height}
}

func (s *Screen) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := 0; i < len(p); i++ {
		if p[i] == 0x1b && i+1 < len(p) && p[i+1] == '[' {
			i += 2
			cmd := ""
			for i < len(p) && ((p[i] >= 0x30 && p[i] <= 0x3f) || (p[i] >= 0x20 && p[i] <= 0x2f)) {
				cmd += string(p[i])
				i++
			}
			if i < len(p) {
				cmd += string(p[i])
			}
			s.handleEscapeSequence(cmd)
		} else if p[i] == '\n' {
			s.cursorY++
			s.cursorX = 0
		} else if p[i] == '\r' {
			s.cursorX = 0
		} else {
			if s.cursorX < s.width && s.cursorY < s.height {
				s.content[s.cursorY][s.cursorX] = rune(p[i])
				s.cursorX++
			}
		}
		if s.cursorX >= s.width {
			s.cursorY++
			s.cursorX = 0
		}
		if s.cursorY >= s.height {
			s.scrollUp()
		}
	}
	return len(p), nil
}

func (s *Screen) handleEscapeSequence(cmd string) {
	switch {
	case cmd == "H":
		s.cursorX, s.cursorY = 0, 0
	case cmd == "2J":
		for i := range s.content {
			for j := range s.content[i] {
				s.content[i][j] = ' '
			}
		}
	case strings.HasSuffix(cmd, "H"):
		parts := strings.Split(strings.TrimSuffix(cmd, "H"), ";")
		if len(parts) == 2 {
			y, _ := strconv.Atoi(parts[0])
			x, _ := strconv.Atoi(parts[1])
			s.cursorY = y - 1
			s.cursorX = x - 1
		}
	}
}

func (s *Screen) scrollUp() {
	copy(s.content, s.content[1:])
	s.content[s.height-1] = make([]rune, s.width)
	for i := range s.content[s.height-1] {
		s.content[s.height-1][i] = ' '
	}
	s.cursorY = s.height - 1
}

func (s *Screen) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	var result []string
	var currentLine strings.Builder
	inEscapeSequence := false

	for _, row := range s.content {
		for _, ch := range row {
			if ch == '\x1b' {
				inEscapeSequence = true
				currentLine.Reset()
			} else if inEscapeSequence {
				if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') {
					inEscapeSequence = false
				}
			} else if ch == '\r' {
				currentLine.Reset()
			} else {
				currentLine.WriteRune(ch)
			}
		}
		if currentLine.Len() > 0 {
			result = append(result, strings.TrimRight(currentLine.String(), " \t"))
			currentLine.Reset()
		}
	}

	// Remove empty lines from the end
	for len(result) > 0 && strings.TrimSpace(result[len(result)-1]) == "" {
		result = result[:len(result)-1]
	}

	return strings.Join(result, "\n")
}

func Execute(command string) (string, error) {
	args := strings.Fields(command)
	if len(args) == 0 {
		return "", fmt.Errorf("empty command")
	}

	cmd := exec.Command(args[0], args[1:]...)

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return "", fmt.Errorf("error creating pseudo-terminal: %v", err)
	}
	defer ptmx.Close()

	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return "", fmt.Errorf("error getting terminal size: %v", err)
	}

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	go func() {
		for range ch {
			if w, h, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
				pty.Setsize(ptmx, &pty.Winsize{Rows: uint16(h), Cols: uint16(w)})
			}
		}
	}()
	ch <- syscall.SIGWINCH

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return "", fmt.Errorf("error setting raw mode: %v", err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	screen := NewScreen(width, height)
	var wg sync.WaitGroup

	// Handle input
	wg.Add(1)
	go func() {
		//defer wg.Done()
		io.Copy(ptmx, os.Stdin)
	}()

	// Handle output
	wg.Add(1)
	go func() {
		defer wg.Done()
		io.Copy(io.MultiWriter(os.Stdout, screen), ptmx)
	}()

	// Wait for the command to finish
	cmdDone := make(chan struct{})
	go func() {
		cmd.Wait()
		close(cmdDone)
	}()

	// Wait for either the command to finish or input to end
	go func() {
		<-cmdDone
    	ptmx.Close()
		wg.Done() // Decrement the wait group when the command is done
	}()

	// Wait for goroutines to finish
	wg.Wait()

	term.Restore(int(os.Stdin.Fd()), oldState)

	return screen.String(), nil
}

// func main() {
// 	if len(os.Args) < 2 {
// 		fmt.Println("Usage: go run aprocess.go <command>")
// 		os.Exit(1)
// 	}

// 	command := strings.Join(os.Args[1:], " ")
// 	output, err := Execute(command)
// 	if err != nil {
// 		fmt.Printf("Error executing command: %v\n", err)
// 		os.Exit(1)
// 	}

// 	fmt.Println("Captured output:")
// 	fmt.Println(output)
// }
