// Command gen prints 2000 item lines followed by a TOTAL line.
package main

import "fmt"

func main() {
	total := 0
	for i := 1; i <= 2000; i++ {
		v := i * 3
		total += v
		fmt.Printf("item-%04d value=%d running_total=%d\n", i, v, total)
	}
	fmt.Printf("TOTAL=%d\n", total)
}
