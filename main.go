package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/creditdb/go-creditdb"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"syscall"
)

type DBClient struct {
	*creditdb.CreditDB
}
type Message struct {
	Sender    string    `json:"sender"`
	Recipient string    `json:"recipient"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

type Router struct {
	engine   *gin.Engine
	dbclient *DBClient
}
type Client struct {
	conn *websocket.Conn
}

var broadcast = make(chan Message)
var userConnections = make(map[string]*Client)
var userConnectionsMutex = &sync.Mutex{}
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func main() {
	r := Router{gin.Default(), &DBClient{creditdb.NewClient().WithPage(10)}}

	router := r.engine
	router.GET("/ws", r.handleWS)
	router.POST("/send", r.sendMessage)
	go broadcastMessages()

	server := &http.Server{
		Addr:    ":8000",
		Handler: router,
	}
	go func() {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
		<-stop

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Println("Server shutdown error: ", err)
			return
		}
		if err := r.dbclient.Close(ctx); err != nil {
			log.Println("DB close error: ", err)
			return
		}
		log.Println("Server stopped gracefully")
	}()
	log.Println("Server is running🎉🎉. Press Ctrl+C to stop")
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Println("Server error: ", err)
		return
	}
}

func (r *Router) handleWS(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Println(err)
		return
	}
	defer conn.Close()
	sender := c.Query("sender")
	recipient := c.Query("recipient")

	if sender == "" || recipient == "" {
		log.Println("sender or recipient is empty")
		return
	}

	db := r.dbclient
	if err := db.SetUserOnline(recipient); err != nil {
		log.Println(err)
		return
	}
	defer db.SetUserOffline(recipient)
	userConnectionsMutex.Lock()
	userConnections[recipient] = &Client{conn}
	userConnectionsMutex.Unlock()

	m := Message{Recipient: recipient, Sender: sender}
	messages, err := db.RetrieveStoredMessages(m)
	if err != nil {
		log.Println(err)
		return
	}

	for _, message := range messages {
		jsonMessage, err := json.Marshal(message)
		if err != nil {
			log.Println(err)
			return
		}
		if err := conn.WriteMessage(websocket.TextMessage, []byte(jsonMessage)); err != nil {
			log.Println(err)
			return
		}
	}

	defer func() {
		userConnectionsMutex.Lock()
		delete(userConnections, recipient)
		userConnectionsMutex.Unlock()
	}()
	for {
		var message Message
		if err := conn.ReadJSON(&message); err != nil {
			log.Println(err)
			return
		}
		broadcast <- message
	}
}

func (r *Router) sendMessage(c *gin.Context) {
	var req struct {
		Sender    string `json:"sender" binding:"required"`
		Recipient string `json:"recipient" binding:"required"`
		Content   string `json:"content" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Println(err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	message := Message{}
	message.Content = req.Content
	message.Recipient = req.Recipient
	message.Timestamp = time.Now()
	message.Sender = req.Sender
	broadcast <- message
	db := r.dbclient
	if err := db.StoreMessage(message); err != nil {
		log.Println(err)
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func broadcastMessages() {
	for {
		msg := <-broadcast
		recipient := msg.Recipient
		if conn, ok := userConnections[recipient]; ok {
			err := conn.conn.WriteJSON(msg)
			if err != nil {
				log.Println(err)
				conn.conn.Close()
				delete(userConnections, recipient)
			}
		}
	}
}

func (db *DBClient) SetUserOnline(userid string) error {
	onlineUsers, err := db.GetLine(context.Background(), "online_users")
	if err != nil {
		if err != creditdb.ErrNotFound {
			return err
		}

	}
	oUsers := []string{}
	if onlineUsers != nil {
		if err := json.Unmarshal([]byte(onlineUsers.Value), &oUsers); err != nil {
			return err
		}

	}
	contains := func() bool {
		for _, user := range oUsers {
			if user == userid {
				return true
			}
		}
		return false
	}
	if !contains() {
		oUsers = append(oUsers, userid)
	}
	data, err := json.Marshal(oUsers)
	if err != nil {

		return err
	}

	if err := db.SetLine(context.Background(), "online_users", string(data)); err != nil {

		return err
	}
	return nil
}

func (db *DBClient) SetUserOffline(userid string) error {
	onlineUsers, err := db.GetLine(context.Background(), "online_users")
	if err != nil {
		return err
	}
	oUsers := []string{}

	if onlineUsers != nil {
		if err := json.Unmarshal([]byte(onlineUsers.Value), &oUsers); err != nil {
			return err
		}
	}

	for i, user := range oUsers {
		if user == userid {
			oUsers = append(oUsers[:i], oUsers[i+1:]...)
			break
		}
	}
	data, err := json.Marshal(oUsers)
	if err != nil {
		return err
	}
	if err := db.SetLine(context.Background(), "online_users", string(data)); err != nil {
		return err
	}
	return nil
}

func (db *DBClient) GetUsersOnline() ([]string, error) {
	onlineUsers, err := db.GetLine(context.Background(), "online_users")
	if err != nil {
		return nil, err
	}
	oUsers := []string{}
	if err := json.Unmarshal([]byte(onlineUsers.Value), &oUsers); err != nil {
		return nil, err
	}
	return oUsers, nil
}

func (db *DBClient) StoreMessage(message Message) error {
	messages, err := db.GetLine(context.Background(), "user:messages:"+message.Sender+":"+message.Recipient)
	if err != nil {
		if err != creditdb.ErrNotFound {
			return err
		}
	}
	mess := []Message{}
	if messages != nil {
		if err := json.Unmarshal([]byte(messages.Value), &mess); err != nil {
			return err
		}
	}
	mess = append(mess, message)
	data, err := json.Marshal(mess)
	if err != nil {
		return err
	}
	if err := db.SetLine(context.Background(), "user:messages:"+message.Sender+":"+message.Recipient, string(data)); err != nil {
		return err
	}
	return nil
}

func (db *DBClient) RetrieveStoredMessages(m Message) ([]Message, error) {
	mess, err := db.GetLine(context.Background(), "user:messages:"+m.Sender+":"+m.Recipient)
	if err != nil {
		if err != creditdb.ErrNotFound {
			return nil, err
		}
	}
	messages := []Message{}
	if mess != nil {
		if err := json.Unmarshal([]byte(mess.Value), &messages); err != nil {
			return nil, err
		}
	}

	return messages, nil
}
