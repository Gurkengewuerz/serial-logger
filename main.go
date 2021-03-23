package main

import (
	"bufio"
	"fmt"
	"github.com/akamensky/argparse"
	"github.com/tarm/serial"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

var (
	TimeDiff = float64(0)
	isPi     = false
	dataChan = make(chan string, 1)
)

func getNow() string {
	// 2006-01-02T15:04:05-0700
	return time.Now().Add(time.Duration(TimeDiff) * time.Second).Format("%FT%T%z")
}

func runForPort(i int, config *serial.Config) bool {
	stream, err := serial.OpenPort(config)
	if err != nil {
		log.Printf("Failed to open port %v: %v", config.Name, err)
		return false
	}
	defer stream.Close()

	f, err := os.OpenFile(fmt.Sprintf("differentmind_%v.log", i), os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Printf("error opening file: %v", err)
		return false
	}
	defer f.Close()

	_, _ = f.WriteString("---------------- new logging started ----------------")

	if isPi {
		exec.Command("gpio", "write", string(rune(8+i)), "1")
	}

	scanner := bufio.NewScanner(stream)
	for scanner.Scan() {
		text := scanner.Text()
		cleanText := strings.Replace(text, "# ", "", -1)
		cleanText = strings.Replace(cleanText, "\r", "", -1)
		cleanText = strings.Replace(cleanText, "\n", "", -1)

		t, err := time.Parse("2006-01-02 15:04:05", cleanText)
		if err == nil {
			log.Println("---------------- syncing time ----------------")
			TimeDiff = time.Now().Sub(t).Seconds()
		}

		log.Print(text)

		withDate := fmt.Sprintf("[%v] %v", getNow(), text)
		_, _ = f.WriteString(withDate)
		dataChan <- fmt.Sprintf("{%v}%v", i, withDate)
	}

	_, _ = f.WriteString("---------------- logging ended ----------------\r\n\r\n\r\n")

	if isPi {
		exec.Command("gpio", "write", string(rune(8+i)), "0")
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Failed to read port %v: %v", config.Name, err)
		return false
	}
	return true
}

func whileRun(i int, config *serial.Config) {
	if isPi {
		exec.Command("gpio", "mode", string(rune(8+i)), "out")
	}

	for {
		runForPort(i, config)
		time.Sleep(5 * time.Second)
	}
}

func main() {
	parser := argparse.NewParser("seriallogger", "by Niklas Schütrumpf <niklas@mc8051.de>")
	ports := parser.List("p", "port", &argparse.Options{Required: true, Help: "COM ports which should be scanned"})
	pi := parser.Flag("g", "gpio", &argparse.Options{Required: false, Help: "If RPi GPIO are supported use this flag"})

	err := parser.Parse(os.Args)
	if err != nil {
		log.Print(parser.Usage(err))
		os.Exit(0)
	}

	isPi = *pi

	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		os.Exit(1)
	}()

	l, err := net.Listen("tcp", "0.0.0.0:9669")
	if err != nil {
		log.Print(err)
		os.Exit(0)
	}
	log.Printf("listening on http://%v", l.Addr())

	cs := newChatServer(dataChan)
	s := &http.Server{
		Handler:      cs,
		ReadTimeout:  time.Second * 10,
		WriteTimeout: time.Second * 10,
	}
	errc := make(chan error, 1)
	go func() {
		errc <- s.Serve(l)
	}()

	for i, s := range *ports {
		if i >= 3 {
			continue
		}

		go whileRun(i, &serial.Config{Name: s, Baud: 9600})
	}

	for {
		time.Sleep(10 * time.Second)
	}
}
