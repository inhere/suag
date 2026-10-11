// Command ci prints a long, noisy simulated CI log with one failing step
// buried in the middle, so the full output has to be read to find it.
package main

import "fmt"

func main() {
	for i := 1; i <= 4000; i++ {
		if i == 2731 {
			fmt.Printf("[step %04d] lint ./internal/config ... FAILED: unused variable cfgPath declared at loader.go:88\n", i)
			continue
		}
		fmt.Printf("[step %04d] check package pkg/mod%03d ... ok (%d files, 0 warnings)\n", i, i%997, i%17+1)
	}
	fmt.Println("CI finished: 1 step failed")
}
