package gateway

import (
	"os"
	"os/signal"
	"syscall"
)

func WaitForInterrupt() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
	<-ch
}
