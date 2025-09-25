package main

import "fmt"

func main() {
	x := 1
	if x > 0 {
		fmt.Println("positive")
		return
	}

	var y int

	y = 2

	go func() {
		fmt.Println(y)
	}()
}