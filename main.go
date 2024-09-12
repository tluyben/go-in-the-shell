package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type Cell struct {
	content string
	result  string
}

type App struct {
	cells       []Cell
	currentCell int
	app         *tview.Application
	textView    *tview.TextView
	inputField  *tview.InputField
	darkMode    bool
}

func NewApp(darkMode bool) *App {
	return &App{
		cells:       []Cell{{content: "", result: ""}},
		currentCell: 0,
		app:         tview.NewApplication(),
		darkMode:    darkMode,
	}
}

func (a *App) Run() error {
	a.textView = tview.NewTextView().
		SetDynamicColors(true).
		SetRegions(true).
		SetChangedFunc(func() {
			a.app.Draw()
		})

	a.inputField = tview.NewInputField().
		SetLabel("Edit: ").
		SetDoneFunc(func(key tcell.Key) {
			if key == tcell.KeyEnter {
				a.cells[a.currentCell].content = a.inputField.GetText()
				a.executeCurrentCell()
				a.app.SetRoot(a.textView, true)
			} else if key == tcell.KeyEsc {
				a.app.SetRoot(a.textView, true)
			}
		})

	// Set colors based on mode
	if a.darkMode {
		a.textView.SetTextColor(tcell.ColorWhite).SetBackgroundColor(tcell.ColorBlack)
		a.inputField.SetFieldTextColor(tcell.ColorWhite).
			SetFieldBackgroundColor(tcell.ColorBlack).
			SetLabelColor(tcell.ColorWhite)
	} else {
		a.textView.SetTextColor(tcell.ColorBlack).SetBackgroundColor(tcell.ColorWhite)
		a.inputField.SetFieldTextColor(tcell.ColorBlack).
			SetFieldBackgroundColor(tcell.ColorWhite).
			SetLabelColor(tcell.ColorBlack)
	}

	a.textView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyUp:
			a.moveUp()
		case tcell.KeyDown:
			a.moveDown()
		case tcell.KeyEnter:
			a.executeCurrentCell()
		case tcell.KeyRune:
			switch event.Rune() {
			case ' ':
				a.editInline()
			case 'v':
				a.editWithVim()
			case '+':
				a.copyCurrentCell()
			}
		}
		return event
	})

	a.updateView()

	return a.app.SetRoot(a.textView, true).Run()
}

func (a *App) updateView() {
	a.textView.Clear()
	totalLines := 0
	selectedCellStart := 0
	for i, cell := range a.cells {
		if i == a.currentCell {
			selectedCellStart = totalLines
		}
		fmt.Fprintf(a.textView, "[\"cell-%d\"][%d]:[\"\"]\n", i+1, i+1)
		totalLines++
		contentLines := strings.Count(cell.content, "\n") + 1
		resultLines := strings.Count(cell.result, "\n") + 1
		fmt.Fprintf(a.textView, "%s", cell.content)
		totalLines += contentLines
		if cell.result != "" {
			fmt.Fprintf(a.textView, "\n\n%s", cell.result)
			totalLines += resultLines + 2
		}
		fmt.Fprintf(a.textView, "\n\n")
		totalLines += 2
	}
	a.textView.Highlight(fmt.Sprintf("cell-%d", a.currentCell+1))
	
	// Calculate the scroll position
	_, _, _, viewHeight := a.textView.GetInnerRect()
	scrollPosition := selectedCellStart - viewHeight/2
	if scrollPosition < 0 {
		scrollPosition = 0
	}
	a.textView.ScrollTo(scrollPosition, 0)
}

func (a *App) moveUp() {
	if a.currentCell > 0 {
		a.currentCell--
		a.updateView()
	}
}

func (a *App) moveDown() {
	if a.currentCell < len(a.cells)-1 {
		a.currentCell++
		a.updateView()
	} else if a.currentCell == len(a.cells)-1 && (a.cells[a.currentCell].content != "" || a.cells[a.currentCell].result != "") {
		// If we're at the last cell and it's not empty, create a new cell
		a.cells = append(a.cells, Cell{content: "", result: ""})
		a.currentCell++
		a.updateView()
	}
}
func (a *App) executeCurrentCell() {
	cell := &a.cells[a.currentCell]
	interpreter := "bash"
	content := cell.content

	if strings.HasPrefix(content, "#") {
		parts := strings.SplitN(content, "\n", 2)
		if len(parts) == 2 {
			interpreter = strings.TrimPrefix(parts[0], "#")
			content = parts[1]
		}
	}

	cmd := exec.Command(interpreter, "-c", content)
	output, err := cmd.CombinedOutput()
	if err != nil {
		cell.result = fmt.Sprintf("Error: %v\n%s", err, output)
	} else {
		cell.result = string(output)
	}

	lastCell := &a.cells[len(a.cells)-1]
	if lastCell.content != "" || lastCell.result != "" {
		a.cells = append(a.cells, Cell{content: "", result: ""})
	}

	a.currentCell = len(a.cells) - 1
	a.updateView()
}

func (a *App) editInline() {
	a.inputField.SetText(a.cells[a.currentCell].content)
	a.app.SetRoot(a.inputField, true)
}

func (a *App) editWithVim() {
	cell := &a.cells[a.currentCell]
	tmpfile, err := os.CreateTemp("", "cell-*.txt")
	if err != nil {
		cell.result = fmt.Sprintf("Error creating temp file: %v", err)
		a.updateView()
		return
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(cell.content)); err != nil {
		cell.result = fmt.Sprintf("Error writing to temp file: %v", err)
		a.updateView()
		return
	}
	tmpfile.Close()

	cmd := exec.Command("vim", tmpfile.Name())
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	a.app.Suspend(func() {
		cmd.Run()
	})

	content, err := os.ReadFile(tmpfile.Name())
	if err != nil {
		cell.result = fmt.Sprintf("Error reading edited content: %v", err)
	} else {
		cell.content = string(content)
		a.executeCurrentCell()
	}

	a.updateView()
}

func (a *App) copyCurrentCell() {
	newCell := a.cells[a.currentCell]
	newCell.result = ""
	a.cells = append(a.cells, newCell)
	a.currentCell = len(a.cells) - 1
	a.updateView()
}

func main() {
	// Define command-line flags
	lightMode := flag.Bool("light", true, "Use light mode (default)")
	darkMode := flag.Bool("dark", false, "Use dark mode")

	// Parse command-line flags
	flag.Parse()

	// Determine the mode
	useDarkMode := *darkMode || !*lightMode

	// Create and run the app
	app := NewApp(useDarkMode)
	if err := app.Run(); err != nil {
		fmt.Printf("Error running application: %v\n", err)
		os.Exit(1)
	}
}