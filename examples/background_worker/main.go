package main

import (
	"context"
	"fmt"
	"time"

	"github.com/aponysus/recourse/recourse"
	"github.com/aponysus/recourse/retry"
)

func main() {
	// Initialize global defaults
	recourse.Init(retry.NewDefaultExecutor())

	fmt.Println("Running background task...")
	err := recourse.Do(context.Background(), "background.task", func(ctx context.Context) error {
		// Simulate flaky work
		fmt.Print(" - Doing work... ")
		if time.Now().UnixNano()%3 != 0 { // Fail 2/3 times
			fmt.Println("Failed!")
			return fmt.Errorf("random failure")
		}
		fmt.Println("Succeeded!")
		return nil
	})

	if err != nil {
		fmt.Printf("Task failed final: %v\n", err)
	} else {
		fmt.Println("Task completed successfully")
	}
}
