package main

import (
	stdhex "encoding/hex"
	"fmt"
	"os"
)

func main() {
	str := stdhex.EncodeToString([]byte(os.Args[1]))
	fmt.Println(str)
}
