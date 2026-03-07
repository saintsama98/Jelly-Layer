//go:build ignore

package main

import (
	"fmt"
	"os"

	"github.com/ethereum/go-ethereum/crypto"
)

func main() {
	var event string
	if len(os.Args) > 1 {
		event = os.Args[1]
	} else {
		event = "PrioritiesSubmitted(uint256,uint256,uint256)"
	}
	h := crypto.Keccak256Hash([]byte(event))
	fmt.Println(h.Hex())
}
