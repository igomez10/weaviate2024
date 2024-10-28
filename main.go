package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/weaviate/weaviate-go-client/v4/weaviate"
	"github.com/weaviate/weaviate-go-client/v4/weaviate/auth"
	"github.com/weaviate/weaviate-go-client/v4/weaviate/graphql"
	"github.com/weaviate/weaviate/entities/models"
)

// Best practice: store your credentials in environment variables
// WEAVIATE_URL       your Weaviate instance URL
// WEAVIATE_API_KEY   your Weaviate instance API key

// Create the client
func CreateClient() *weaviate.Client {
	cfg := weaviate.Config{
		Host:       os.Getenv("WEAVIATE_URL"),
		Scheme:     "https",
		AuthConfig: auth.ApiKey{Value: os.Getenv("WEAVIATE_API_KEY")},
		// pass openapikey
		Headers: map[string]string{
			"X-OpenAI-Api-Key": os.Getenv("OPENAI_API_KEY"),
		},
	}

	client, err := weaviate.NewClient(cfg)
	if err != nil {
		fmt.Println(err)
	}

	// Check the connection
	if _, err := client.Misc().LiveChecker().Do(context.Background()); err != nil {
		panic(err)
	}

	return client
}

type message struct {
	Sender  string `json:"sender"`
	Message string `json:"message"`
}

var className = "Message"

func main() {
	// load env from .env file
	if err := godotenv.Load(); err != nil {
		panic(err)
	}

	// fmt.Print("WEAVIATE_URL: ", os.Getenv("WEAVIATE_URL"), "\n")
	// fmt.Print("WEAVIATE_API_KEY: ", os.Getenv("WEAVIATE_API_KEY"), "\n")

	c := CreateClient()

	// res, err := c.Cluster().NodesStatusGetter().Do(context.Background())
	// if err != nil {
	// 	panic(err)
	// }

	// fmt.Printf("Nodes status: %+v \n", res)

	// load file from chathistory.json
	file, err := os.Open("chathistory.json")
	if err != nil {
		panic(err)
	}

	// decode json file
	var messages []message
	if err := json.NewDecoder(file).Decode(&messages); err != nil {
		panic(err)
	}

	postMessages(messages, c)
	// listItems(c)
	// panic("done")
	// search messages related to "books"
	incomingMessage := "Michael: What is the name of the movie that you recently watched and you told me about?"
	fmt.Printf("\nIncoming message: %q \n\n", incomingMessage)
	querybuilder := &graphql.NearTextArgumentBuilder{}
	querybuilder.WithConcepts([]string{incomingMessage})
	response, err := c.GraphQL().Get().
		WithClassName(className).
		WithFields(
			graphql.Field{Name: "message"},
			graphql.Field{Name: "sender"},
		).
		WithNearText(querybuilder).
		WithLimit(10).
		Do(context.Background())

	if err != nil {
		panic(err)
	}

	if response.Errors != nil {
		for _, e := range response.Errors {
			fmt.Printf("Error: %+v \n", e)
		}
		return
	}

	fmt.Print("RAG Results: \n")
	for _, obj := range response.Data {
		fmt.Printf("Message: %s \n", obj)
	}
	fmt.Print("End Rag Results\n")

	// fmt.Printf("Messages related to %s: %+v \n", incomingMessage, response)
	answer := generateAnswer(incomingMessage, fmt.Sprintf("%+v", response))

	fmt.Println(answer)
}

func postMessages(messages []message, c *weaviate.Client) {
	objs := []*models.Object{}
	for i, _ := range messages {
		messageChunk := []string{}
		for j := i; j < i+3 && j < len(messages); j++ {
			messageChunk = append(messageChunk, messages[j].Message, messages[j].Sender)
		}

		obj := &models.Object{
			Class: className,
			Properties: map[string]interface{}{
				"message": strings.Join(messageChunk, "\n"),
			},
		}
		objs = append(objs, obj)
	}
	postRes, err := c.Batch().
		ObjectsBatcher().
		WithObjects(objs...).
		Do(context.Background())
	if err != nil {
		panic(err)
	}

	for i := range postRes {
		if postRes[i].Result.Errors != nil {
			fmt.Printf("Error: %+v \n", postRes[i].Result.Errors)
		}
	}
}

func listItems(c *weaviate.Client) {
	res, err := c.GraphQL().Get().
		WithClassName(className).
		WithFields(
			graphql.Field{Name: "message"},
			// graphql.Field{Name: "sender"},
		).
		Do(context.Background())

	if err != nil {
		panic(err)
	}

	fmt.Println("Messages:", res)
}

func generateAnswer(incomingMessage, ragResults string) string {
	userPrompt := `
	
	Incoming message: 
	"""
	 %s
	"""

	Previous conversations:
	"""
	%s
	"""
	`

	userPrompt = fmt.Sprintf(userPrompt, incomingMessage, ragResults)

	systemPrompt := `
	You are a helpful assistant and you generate responses based on the conversation context and tone.
	The reply should answering the incoming message directly, be short and concise.
	`

	// Replace this with your GROQ API key
	apiKey := os.Getenv("GROQ_API_KEY")

	// The URL for the Groq OpenAI endpoint
	url := "https://api.groq.com/openai/v1/chat/completions"

	// Define the request payload
	payload := map[string]interface{}{
		"messages": []map[string]string{
			{
				"role":    "system",
				"content": systemPrompt,
			},
			{
				"role":    "user",
				"content": userPrompt,
			},
		},
		"model": "llama-3.1-70b-versatile",
	}

	// Encode the payload to JSON
	jsonData, err := json.Marshal(payload)
	if err != nil {
		log.Fatalf("Error encoding JSON: %v", err)
	}

	// Create a new HTTP request
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Fatalf("Error creating request: %v", err)
	}

	// Set headers
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	// Send the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("Error making request: %v", err)
	}
	defer resp.Body.Close()

	// Read the response
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Error reading response: %v", err)
	}

	// unmashall the response
	var groqResponse GroqResponse
	err = json.Unmarshal(body, &groqResponse)
	if err != nil {
		log.Fatalf("Error unmarshalling response: %v", err)
	}

	// Output the response
	// for i, choice := range groqResponse.Choices {
	// 	fmt.Printf("Response %d: %s\n\n", i, choice.Message.Content)
	// }

	return groqResponse.Choices[0].Message.Content
}

type GroqResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int    `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		Logprobs     any    `json:"logprobs"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		QueueTime        float64 `json:"queue_time"`
		PromptTokens     int     `json:"prompt_tokens"`
		PromptTime       float64 `json:"prompt_time"`
		CompletionTokens int     `json:"completion_tokens"`
		CompletionTime   float64 `json:"completion_time"`
		TotalTokens      int     `json:"total_tokens"`
		TotalTime        float64 `json:"total_time"`
	} `json:"usage"`
	SystemFingerprint string `json:"system_fingerprint"`
	XGroq             struct {
		ID string `json:"id"`
	} `json:"x_groq"`
}
