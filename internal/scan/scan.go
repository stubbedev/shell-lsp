package scan

import "fmt"

func Forward(start, end int, step func(at int) int) {
	for at := start; at < end; {
		next := step(at)
		if next <= at {
			panic(fmt.Sprintf("scan: the step at offset %d went to %d instead of forward", at, next))
		}
		at = next
	}
}
