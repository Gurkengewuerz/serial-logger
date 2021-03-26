package main

import (
	"bytes"
	"fmt"
	"github.com/akamensky/argparse"
	"go.bug.st/serial"
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
	TimeDiff   = float64(0)
	isPi       = false
	dataChan   = make(chan string)
	deleteChan = make(chan int)
)

func getNow() string {
	// 2006-01-02T15:04:05-0700
	return time.Now().Add(time.Duration(TimeDiff) * time.Second).Format(time.RFC3339)
}

func runForPort(i int, portName string, mode *serial.Mode) bool {

	port, err := serial.Open(portName, mode)
	if err != nil {
		log.Printf("Failed to open port %v: %v", portName, err)
		return false
	}

	defer port.Close()

	f, err := os.OpenFile(fmt.Sprintf("differentmind_%v.txt", i), os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Printf("error opening file: %v", err)
		return false
	}
	defer f.Close()

	_, _ = f.WriteString("---------------- new logging started ----------------\r\n")

	if isPi {
		exec.Command("gpio", "write", string(rune(8+i)), "1")
	}

	readError := false
	delFile := false
	stringBuffer := bytes.NewBufferString("")
	stringBuffer.Grow(1024)
	buf := make([]byte, 512)

	go func() {
		for {
			toDel := <-deleteChan
			//log.Print("received message", toDel)
			if toDel == i {
				delFile = true
				return
			}
		}
	}()

	for {
		if delFile {
			f.Close()
			_ = os.Remove(fmt.Sprintf("differentmind_%v.txt", i))
			return true
		}

		n, err := port.Read(buf)
		if err != nil || n == 0 {
			readError = true
			break
		}
		stringBuffer.WriteString(string(buf[:n]))

		if strings.Contains(stringBuffer.String(), "\n") {
			s, err := stringBuffer.ReadString('\n')
			cleanText := strings.Replace(s, "\r", "", -1)
			cleanText = strings.Replace(cleanText, "\n", "", -1)

			if cleanText[0] == 0x00 {
				cleanText = cleanText[1:]
			}

			withDate := fmt.Sprintf("[%v] %v", getNow(), cleanText)
			_, _ = f.WriteString(withDate)
			_, _ = f.WriteString("\n")
			f.Sync()

			wsMsg := fmt.Sprintf("{%v}%v", i, withDate)

			select {
			case dataChan <- wsMsg:
				//fmt.Println("sent message")
			default:
				//fmt.Println("no message sent")
			}

			t, err := time.Parse("2006-01-02 15:04:05", strings.Replace(cleanText, "# ", "", -1))
			if err == nil {
				log.Println("---------------- syncing time ----------------")
				TimeDiff = time.Now().Sub(t).Seconds()
			}
		}
	}

	_, _ = f.WriteString("---------------- logging ended ----------------\r\n\r\n\r\n")

	if isPi {
		exec.Command("gpio", "write", string(rune(8+i)), "0")
	}

	if readError {
		log.Printf("Failed to read port %v", portName)
		return false
	}
	return true
}

func whileRun(i int, port string, config *serial.Mode) {
	if isPi {
		exec.Command("gpio", "mode", string(rune(8+i)), "out")
	}

	for {
		runForPort(i, port, config)
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

	cs := newChatServer(&dataChan, &deleteChan)
	s := &http.Server{
		Handler:      cs,
		ReadTimeout:  time.Second * 10,
		WriteTimeout: time.Second * 10,
	}
	errc := make(chan error, 1)
	go func() {
		errc <- s.Serve(l)
	}()

	mode := serial.Mode{
		BaudRate: 9600,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}

	for i, s := range *ports {
		if i >= 3 {
			continue
		}

		go whileRun(i, s, &mode)
	}

	for {
		time.Sleep(10 * time.Second)
	}
}
