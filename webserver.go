package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"nhooyr.io/websocket"
)

var (
	//go:embed templates
	templates embed.FS
)

// chatServer enables broadcasting to a set of subscribers.
type chatServer struct {
	// serveMux routes the various endpoints to the appropriate handler.
	serveMux http.ServeMux

	subscribersMu sync.Mutex
	subscribers   map[*subscriber]struct{}

	numbPorts int

	delChan *chan int
}

// newChatServer constructs a chatServer with the defaults.
func newChatServer(c *chan []byte, delChan *chan int, numbPorts int) *chatServer {
	cs := &chatServer{
		subscribers: make(map[*subscriber]struct{}),
		delChan:     delChan,
		numbPorts:   numbPorts,
	}

	serverRoot, err := fs.Sub(templates, "templates")
	if err != nil {
		log.Fatal(err)
	}

	cs.serveMux.Handle("/", http.FileServer(http.FS(serverRoot)))
	cs.serveMux.HandleFunc("/subscribe", cs.subscribeHandler)
	cs.serveMux.HandleFunc("/ports", cs.getPorts)
	cs.serveMux.HandleFunc("/data/", cs.getFile)
	cs.serveMux.HandleFunc("/delete/", cs.deleteFile)

	go func() {
		for {
			data := <-*c
			cs.publish(data)
		}
	}()
	return cs
}

// subscriber represents a subscriber.
// Messages are sent on the msgs channel and if the client
// cannot keep up with the messages, closeSlow is called.
type subscriber struct {
	msgs      chan []byte
	closeSlow func()
}

func (cs *chatServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	cs.serveMux.ServeHTTP(w, r)
}

func (cs *chatServer) getFile(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/data/")
	if _, err := strconv.Atoi(id); err != nil {
		w.WriteHeader(400)
		return
	}

	http.ServeFile(w, r, fmt.Sprintf("differentmind_%v.txt", id))
}

func (cs *chatServer) getPorts(w http.ResponseWriter, r *http.Request) {
	jsonResponse := struct {
		Ports int `json:"ports"`
	}{
		Ports: cs.numbPorts,
	}
	j, _ := json.Marshal(jsonResponse)
	_, _ = w.Write(j)
}

func (cs *chatServer) deleteFile(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/delete/")
	iID, err := strconv.Atoi(id)
	if err != nil {
		w.WriteHeader(400)
		return
	}
	select {
	case *cs.delChan <- iID:
		fmt.Println("sent to channel to delete file")
	default:
		fmt.Println("no message sent")
	}

	w.WriteHeader(200)
}

// subscribeHandler accepts the WebSocket connection and then subscribes
// it to all future messages.
func (cs *chatServer) subscribeHandler(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		log.Printf("%v", err)
		return
	}
	defer c.Close(websocket.StatusInternalError, "")

	err = cs.subscribe(r.Context(), c)
	if errors.Is(err, context.Canceled) {
		return
	}
	if websocket.CloseStatus(err) == websocket.StatusNormalClosure ||
		websocket.CloseStatus(err) == websocket.StatusGoingAway {
		return
	}
	if err != nil {
		log.Printf("%v", err)
		return
	}
}

// subscribe subscribes the given WebSocket to all broadcast messages.
// It creates a subscriber with a buffered msgs chan to give some room to slower
// connections and then registers the subscriber. It then listens for all messages
// and writes them to the WebSocket. If the context is cancelled or
// an error occurs, it returns and deletes the subscription.
//
// It uses CloseRead to keep reading from the connection to process control
// messages and cancel the context if the connection drops.
func (cs *chatServer) subscribe(ctx context.Context, c *websocket.Conn) error {
	ctx = c.CloseRead(ctx)

	s := &subscriber{
		msgs: make(chan []byte, 15),
		closeSlow: func() {
			c.Close(websocket.StatusPolicyViolation, "connection too slow to keep up with messages")
		},
	}
	cs.addSubscriber(s)
	defer cs.deleteSubscriber(s)

	for {
		select {
		case msg := <-s.msgs:
			err := writeTimeout(ctx, time.Second*5, c, msg)
			if err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// publish publishes the msg to all subscribers.
// It never blocks and so messages to slow subscribers
// are dropped.
func (cs *chatServer) publish(msg []byte) {
	cs.subscribersMu.Lock()
	defer cs.subscribersMu.Unlock()

	for s := range cs.subscribers {
		select {
		case s.msgs <- msg:
		default:
			go s.closeSlow()
		}
	}
}

// addSubscriber registers a subscriber.
func (cs *chatServer) addSubscriber(s *subscriber) {
	cs.subscribersMu.Lock()
	cs.subscribers[s] = struct{}{}
	cs.subscribersMu.Unlock()
}

// deleteSubscriber deletes the given subscriber.
func (cs *chatServer) deleteSubscriber(s *subscriber) {
	cs.subscribersMu.Lock()
	delete(cs.subscribers, s)
	cs.subscribersMu.Unlock()
}

func writeTimeout(ctx context.Context, timeout time.Duration, c *websocket.Conn, msg []byte) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return c.Write(ctx, websocket.MessageText, msg)
}
