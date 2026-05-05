package main

import "os"

// main is the process adapter. Its only responsibilities:
//  1. Construct the App (dependency composition root)
//  2. Pass os.Args to App.Run
//  3. Exit with the returned code
//
// No business logic, no system calls, no output formatting belongs here.
// If code in main would need its own unit test, it does not belong in main.
func main() {
	app := NewApp(os.Stdout, os.Stderr)
	code := app.Run(os.Args[1:])
	os.Exit(code)
}