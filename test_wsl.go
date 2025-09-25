package main

import (
	"fmt"
	"context"
)

func main() {
	fmt.Println("test")
	defer fmt.Println("cleanup")
	ctx := context.Background()
	if ctx != nil {
		fmt.Println("context exists")
		return
	}
	var x int
	x = 5
	go func() {
		fmt.Println("goroutine")
	}()
}
