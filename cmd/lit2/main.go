// Command lit2 is the program: it hands its arguments to the root and exits
// with the status it returns.
package main

import (
	"os"

	"github.com/promptctl/lit2/internal/root"
)

func main() { os.Exit(root.Main(os.Args[1:])) }
