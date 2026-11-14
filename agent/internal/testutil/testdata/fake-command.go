package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(os.Args[0])), ".exe")
	switch name {
	case "capture":
		fmt.Fprintln(os.Stdout, "hello stdout")
		fmt.Fprintln(os.Stderr, "token=secret-value")
		fmt.Fprintln(os.Stderr, "verbose stderr")
		os.Exit(3)
	case "stream":
		fmt.Fprintln(os.Stdout, "token=secret-value")
		release := filepath.Join(filepath.Dir(os.Args[0]), "release")
		for {
			if _, err := os.Stat(release); err == nil {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		fmt.Fprint(os.Stdout, "last line without newline")
	case "large":
		fmt.Print(strings.Repeat("x", 70000))
	case "large-streams":
		fmt.Fprint(os.Stdout, strings.Repeat("o", 131072))
		fmt.Fprint(os.Stderr, strings.Repeat("e", 131072))
	case "long-line":
		fmt.Fprintln(os.Stdout, strings.Repeat("x", 12000))
	case "wait", "wait-after-start":
		if name == "wait-after-start" {
			fmt.Fprintln(os.Stdout, "started")
		}
		for {
			time.Sleep(time.Second)
		}
	case "terraform":
		fmt.Fprintln(os.Stdout, strings.Join(os.Args[1:], " "))
	default:
		fmt.Fprintf(os.Stderr, "unknown test command %q\n", name)
		os.Exit(2)
	}
}
