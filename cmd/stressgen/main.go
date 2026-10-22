package main

import (
	"fmt"
	"os"

	"nori/internal/sheetimport"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: stressgen <out.xlsx> <rows>")
		os.Exit(2)
	}
	f, err := os.Create(os.Args[1])
	if err != nil {
		panic(err)
	}
	defer f.Close()

	var n int
	fmt.Sscan(os.Args[2], &n)
	rows := make([][]string, n)
	for i := range rows {
		k := i + 1
		rows[i] = []string{
			fmt.Sprintf("item-%06d", k),
			fmt.Sprintf("cat-%02d", k%20),
			fmt.Sprintf("%d", k*7),
			fmt.Sprintf("note for item %d with keywords alpha bravo charlie delta", k),
		}
	}
	if err := sheetimport.Write(f, "stress", []string{"name", "category", "value", "note"}, rows); err != nil {
		panic(err)
	}
}
