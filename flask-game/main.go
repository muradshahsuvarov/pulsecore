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

func StartGame(width int, height int) {

	// Initialize the flask with air
	flask := make([][]string, height)
	for i := range flask {
		flask[i] = make([]string, width)
		for j := range flask[i] {
			flask[i][j] = empty
		}
	}

	// Simulation parameters
	speed := time.Millisecond * 200 // Control the speed of water flow

	// Main loop for the water flow
	isFull := false
	for !isFull {

		// Let the water drop "fall" to the bottom of the flask
		for i := height - 1; i >= 0; i-- {
			// Randomly choose where the water drop falls
			dropPosition := rand.Intn(width)
			if flask[i][dropPosition] == empty {
				flask[i][dropPosition] = water
				// TODO: Send a message saying {i,dropPostion}
				if !rowIsFull(flask[i]) {
					break
				}
			}
		}

		// Check if the top row is full
		isFull = true
		for j := 0; j < width; j++ {
			if flask[0][j] == empty {
				isFull = false
				break
			}
		}

		// Draw the flask with the water
		drawFlask(flask)
		fmt.Println()

		// Wait for a bit before the next drop
		time.Sleep(speed)
	}

	fmt.Println("The flask is now full!")
}

func rowIsFull(row []string) bool {
	for i := 0; i < len(row); i++ {
		if row[i] == empty {
			return false
		}
	}
	return true
}

// drawFlask renders the flask and its contents to the terminal.
func drawFlask(flask [][]string) {
	// Clear the console - this is platform dependent
	fmt.Print("\033[H\033[2J")

	// Draw the flask from the top to the bottom
	for i := 0; i < len(flask); i++ {
		fmt.Print(flaskSide)
		for j := 0; j < len(flask[i]); j++ {
			fmt.Print(flask[i][j])
		}
		fmt.Println(flaskSide)
	}

	// Draw the base of the flask
	fmt.Printf("%s%s%s\n", flaskSide, strings.Repeat(flaskSide, len(flask[0])), flaskSide)
}
