package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/nicolasacquaviva/nicolasacquaviva.github.io/models"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{}

type HttpResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

type Env struct {
	db models.Datastore
}

func (env *Env) content(w http.ResponseWriter, r *http.Request) {
	var content models.Content

	if r.Method == "GET" {
		params, ok := r.URL.Query()["name"]

		if !ok || len(params[0]) < 1 {
			err := errors.New("'name' query param is required")
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}

		name := params[0]

		content, err := env.db.GetContentByName(name)

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		data, err := json.Marshal(content)

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(data)

	} else if r.Method == "POST" {
		_ = json.NewDecoder(r.Body).Decode(&content)

		newContent, err := env.db.AddContent(content)

		if err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}

		data, err := json.Marshal(newContent)

		if err != nil {
			log.Println("Error creating content:", err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	} else {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func health(w http.ResponseWriter, r *http.Request) {
	response := HttpResponse{}
	response.Success = true
	response.Message = "Api up and running"

	w.Header().Set("access-control-allow-origin", "*")

	data, err := json.Marshal(response)

	if err != nil {
		log.Printf("Error parsing json")
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

func checkOrigin(r *http.Request) bool {
	if isProduction() {
		origin := r.Header.Get("Origin")

		return origin == "https://nicolasacquaviva.com" || origin == "https://www.nicolasacquaviva.com"
	}

	return true
}

func isProduction() bool {
	return os.Getenv("MODE") == "production"
}

func getIP(r *http.Request) string {
	forwardedFor := r.Header.Get("x-forwarded-for")

	if forwardedFor != "" {
		return forwardedFor
	}

	return r.RemoteAddr
}

func (env *Env) ws(w http.ResponseWriter, r *http.Request) {
	upgrader.CheckOrigin = checkOrigin

	c, err := upgrader.Upgrade(w, r, nil)

	if err != nil {
		log.Print("Error upgrading:", err)

		return
	}

	defer c.Close()

	for {
		messageType, message, err := c.ReadMessage()

		if err != nil {
			log.Println("Error reading message:", err)
			break
		}

		messageParts := strings.Split(string(message), ":")

		if isProduction() {
			env.db.SaveCommand(
				messageParts[1],
				messageParts[0] == "command",
				getIP(r), r.Header.Get("user-agent"),
			)
		}

		commandResponse := env.executeCommand(messageParts)

		err = c.WriteMessage(messageType, []byte(commandResponse))

		if err != nil {
			log.Println("Error sending message:", err)
		}
	}
}

// ls
func (env *Env) listDirectory(dir string, params string) string {
	content, err := env.db.GetContentByParentDir(dir)

	if err != nil {
		log.Println("Cannot list directory:", err)
		return ""
	}

	return strings.Join(content[:], " ")
}

// cat
func (env *Env) printFileContent(name string) string {
	if name == "" {
		return "usage: cat [file_name]"
	}

	content := env.db.GetFileContent(name)

	if content == "" {
		return "cat: " + name + ": No such file or directory"
	}

	return content
}

func (env *Env) executeCommand(input []string) string {
	dir := input[0]
	command := input[1]
	params := input[2]

	switch command {
	case "ls":
		return env.listDirectory(dir, params)
	case "cat":
		return env.printFileContent(params)
	case "help":
		return ""
	case "clear":
		return ""
	default:
		return "command not found: " + command + ". Try using the 'help'"
	}
}

func (env *Env) attachHttpHandlers() {
	http.HandleFunc("/content", env.content)
	http.HandleFunc("/health", health)
	http.HandleFunc("/ws", env.ws)
}

func main() {
	db, err := models.NewDB(os.Getenv("MONGODB_URI"))

	if err != nil {
		log.Fatal("DB error", err)
	}

	env := &Env{db}

	env.attachHttpHandlers()

	log.Fatal(http.ListenAndServe(":"+os.Getenv("PORT"), nil))
}
