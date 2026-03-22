package service

import "os"

func getPID() int {
	return os.Getpid()
}
