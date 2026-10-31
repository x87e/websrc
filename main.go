/*
              _                                 
__      _____| |__  ___ _ __ ___   _____  _____ 
\ \ /\ / / _ \ '_ \/ __| '__/ __| / _ \ \/ / _ \
 \ V  V /  __/ |_) \__ \ | | (__ |  __/>  <  __/
  \_/\_/ \___|_.__/|___/_|  \___(_)___/_/\_\___|
                                                 
*/

package main

import (
	"os"
	"websrc/internal/cli"
)

var version = "dev"

func main() {
	executable, err := os.Executable()
	if err != nil {
		executable = os.Args[0]
	}
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, executable, version))
}
