package main

import (
	"bufio"
	"fmt"
	"github.com/akamensky/argparse"
	"github.com/tarm/serial"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"
)

var TIME_DIFF = float64(0)

func getNow() string {
	// 2006-01-02T15:04:05-0700
	return time.Now().Add(time.Duration(TIME_DIFF) * time.Second).Format("%FT%T%z")
}

func runForPort(i int, config *serial.Config) bool {
	stream, err := serial.OpenPort(config)
	if err != nil {
		fmt.Printf("Failed to open port %v: %v\r\n", config.Name, err)
		return false
	}
	defer stream.Close()

	f, err := os.OpenFile(fmt.Sprintf("differentmind_%v.log", i), os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		fmt.Printf("error opening file: %v\r\n", err)
		return false
	}
	defer f.Close()

	_, _ = f.WriteString("---------------- new logging started ----------------")

	if runtime.GOOS == "linux" {
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
			fmt.Println("---------------- syncing time ----------------")
			TIME_DIFF = time.Now().Sub(t).Seconds()
		}

		fmt.Print(text)
		_, _ = f.WriteString(fmt.Sprintf("[%v] %v", getNow(), text))
	}

	_, _ = f.WriteString("---------------- logging ended ----------------\r\n\r\n\r\n")


	if runtime.GOOS == "linux" {
		exec.Command("gpio", "write", string(rune(8+i)), "0")
	}

	if err := scanner.Err(); err != nil {
		fmt.Printf("Failed to read port %v: %v\r\n", config.Name, err)
		return false
	}
	return true
}

func whileRun(i int, config *serial.Config) {
	if runtime.GOOS == "linux" {
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

	err := parser.Parse(os.Args)
	if err != nil {
		fmt.Print(parser.Usage(err))
		os.Exit(0)
	}

	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		os.Exit(1)
	}()

	for i, s := range *ports {
		go whileRun(i, &serial.Config{Name: s, Baud: 9600})
	}

	for {
		time.Sleep(10 * time.Second)
	}
}
