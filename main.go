package main

import (
	"log"
)

func main() {
	mod := &Mod{}
	if err := mod.Init(); err != nil {
		log.Printf("init error: %v", err)
		return
	}
	if file, err := mod.ParseFile("/Users/zdypro/Documents/projects/src/zdypro888/applesys/nserver/account.go"); err != nil {
		log.Printf("parse error: %v", err)
		return
	} else {
		file.ProtoWrite()
		file.ClientWrite()
	}
}
