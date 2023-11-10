package flaskgame

import (
	"fmt"
	"math/rand"
	"strings"
	"time"
)

const (
	flaskSide = "|"
	flaskBase = "_"
	water     = "*"
	empty     = " "
)

type FlaskGame struct{}

var f *FlaskGame = &FlaskGame{}

var Flask [][]string

func StartGame(width *int, height *int, gameFinished *bool, updateFlask *bool) {

	// Initialize the flask with air
	Flask = make([][]string, *height)
	for i := range Flask {
		Flask[i] = make([]string, *width)
		for j := range Flask[i] {
			Flask[i][j] = empty
		}
	}

	// Simulation parameters
	speed := time.Millisecond * 200 // Control the speed of water flow

	// Main loop for the water flow
	isFull := false
	for !isFull {

		// Let the water drop "fall" to the bottom of the flask
		for i := *height - 1; i >= 0; i-- {
			*updateFlask = false
			// Randomly choose where the water drop falls
			dropPosition := rand.Intn(*width)
			if Flask[i][dropPosition] == empty {
				f.UpdateFlask(i, dropPosition)
				*updateFlask = true
				if !RowIsFull(Flask[i]) {
					break
				}
			}
		}

		// Check if the top row is full
		isFull = true
		for j := 0; j < *width; j++ {
			if Flask[0][j] == empty {
				isFull = false
				break
			}
		}

		// Draw the flask with the water
		f.DrawFlask()
		fmt.Println()

		// Wait for a bit before the next drop
		time.Sleep(speed)
	}

	*gameFinished = true
	fmt.Println("The flask is now full!")
}

func RowIsFull(row []string) bool {
	for i := 0; i < len(row); i++ {
		if row[i] == empty {
			return false
		}
	}
	return true
}

// DrawFlask renders the flask and its contents to the terminal.
func (f *FlaskGame) DrawFlask() {
	// Clear the console - this is platform dependent
	fmt.Print("\033[H\033[2J")

	// Draw the flask from the top to the bottom
	for i := 0; i < len(Flask); i++ {
		fmt.Print(flaskSide)
		for j := 0; j < len(Flask[i]); j++ {
			fmt.Print(Flask[i][j])
		}
		fmt.Println(flaskSide)
	}

	// Draw the base of the flask
	fmt.Printf("%s%s%s\n", flaskSide, strings.Repeat(flaskSide, len(Flask[0])), flaskSide)
}

func (f *FlaskGame) UpdateFlask(row int, col int) {
	Flask[row][col] = water
	f.DrawFlask()
}
