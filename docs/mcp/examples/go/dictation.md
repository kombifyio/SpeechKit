```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/kombifyio/SpeechKit/pkg/speechkit/client"
)

func main() {
    ctx := context.Background()

    c, err := client.New(client.Options{
        BaseURL: os.Getenv("SPEECHKIT_SERVER_URL"),
        Token:   os.Getenv("SPEECHKIT_TOKEN"),
    })
    if err != nil {
        panic(err)
    }
    result, err := c.TranscribeFile(ctx, "hello.wav", client.TranscribeOptions{Language: "en"})
    if err != nil {
        panic(err)
    }
    fmt.Println(result.Text)
}
```
