package main

import (
  "bufio"
  "bytes"
  "crypto/tls"
  "encoding/base64"
  "flag"
  "fmt"
  "github.com/spf13/viper"
  "io"
  "io/ioutil"
  "log"
  "mime/multipart"
  "net/http"
  "os"
  "os/exec"
  "path/filepath"
  "strings"
  "time"
)

func init() {
  log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile)
}
func main() {
  url := flag.String("url", "", "remote server url")
  p := flag.String("p", "", "password")
  b := flag.String("b", "", "buidl script")
  a := flag.String("a", "", "action")
  e := flag.String("e", "dev", "environment:such dev,test,prod")
  //filePath := target/malang-pen-api-server-1.0.0.jar
  filePath := flag.String("file", "", "local file path")
  //d := "/data/apps/web/ztbjg"
  m := flag.String("m", "", "move to file path")
  d := flag.String("d", "", "extra file path")
  c := flag.String("c", "", "full command")

  flag.Parse()

  // read config file
  viper.SetConfigFile(".deploy.toml")
  viper.SetConfigType("toml")
  if err := viper.ReadInConfig(); err != nil {
    log.Printf("Error reading config file, %s \n", err)
  }

  if *a == "" {
    *a = "upload-run"
  }
  // read config item
  if *url == "" {
    *url = viper.GetString(*e + "." + *a + ".url")
  }

  if *a == "upload-run" {

    if *b == "" {
      *b = viper.GetString(*e + ".upload-run.b")
    }
    if *filePath == "" {
      *filePath = viper.GetString(*e + ".upload-run.file")
    }

    if *m == "" {
      *m = viper.GetString(*e + ".upload-run.m")
    }

    if *p == "" {
      *p = viper.GetString(*e + ".upload-run.p")
    }

    if *d == "" {
      *d = viper.GetString(*e + ".upload-run.d")
    }

    if *c == "" {
      *c = viper.GetString(*e + "." + *a + ".c")
    }

    log.Println("b:", *b)
    log.Println("url:", *url)
    log.Println("p:", *p)
    log.Println("filePath:", *filePath)
    log.Println("m:", *m)
    log.Println("d:", *d)
    log.Println("c:", *c)
  }

  if *a == "web" {
    if *c == "" {
      *c = viper.GetString(*e + "." + *a + ".c")
    }
    log.Println("url:", *url)
    log.Println("c:", *c)
  }

  start := time.Now().Unix()
  var client *http.Client = &http.Client{
    Transport: &http.Transport{
      TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
    },
  }
  if strings.HasSuffix(*url, "upload-run/") {
    log.Println("action is upload-run")
    if len(*url) == 0 {
      fmt.Println("please specified remote server url")
      return
    }
    if len(*filePath) == 0 {
      fmt.Println("please specified local file path")
      return
    }
    build(b)
    uploadAndRun(client, url, p, filePath, m, d, c)
  } else if strings.HasSuffix(*url, "web/") {
    log.Println("web")
    runRemoteCmd(client, url, c)
  }
  end := time.Now().Unix()
  fmt.Println(end-start, "s")
  fmt.Println("done")
}

func build(filePath *string) {
  file, err := os.Open(*filePath)
  if err != nil {
    log.Fatalln("Error opening file:", err)
  }
  defer file.Close()

  envVariables := []string{}

  scanner := bufio.NewScanner(file)
  for scanner.Scan() {
    line := scanner.Text()

    // 检查是否是设置环境变量的命令
    if strings.HasPrefix(line, "set ") {
      value := line[4:]
      envVariables = append(envVariables, value)
      log.Println("add env variable:", value)
      continue // 跳过执行此命令
    }

    executeCommand(line, envVariables)
  }

  if err := scanner.Err(); err != nil {
    log.Fatalln("Error reading file:", err)
  }

}

func uploadAndRun(client *http.Client, url *string, p *string, filePath *string, m *string, d *string, c *string) {
  var file, errFile1 = os.Open(*filePath)
  if errFile1 != nil {
    log.Fatalln(errFile1)
  }
  defer file.Close()
  var fileInfo, err = file.Stat()
  if err != nil {
    log.Fatalln(err)
  }
  var fileSize = fileInfo.Size()

  log.Printf("file is uploading,and file size is %d KB", fileSize/1024)

  method := "POST"
  payload := &bytes.Buffer{}

  // io.Writer
  writer := multipart.NewWriter(payload)
  part1, errFile1 := writer.CreateFormFile("file", filepath.Base(*filePath))
  _, errFile1 = io.Copy(part1, file)
  if errFile1 != nil {
    log.Fatalln(errFile1)
    return
  }

  if len(*p) != 0 {
    _ = writer.WriteField("p", *p)
  }
  if len(*m) != 0 {
    _ = writer.WriteField("m", *m)
  }

  if len(*d) != 0 {
    _ = writer.WriteField("d", *d)
  }

  if len(*c) != 0 {
    _ = writer.WriteField("c", *c)
  }

  err = writer.Close()
  if err != nil {
    fmt.Println(err)
    return
  }

  req, err := http.NewRequest(method, *url, payload)
  if err != nil {
    log.Fatalln(err)
  }
  req.Header.Set("Content-Type", writer.FormDataContentType())

  res, err := client.Do(req)
  if err != nil {
    fmt.Println("Faild to send http request", err)
    return
  }
  code := res.StatusCode
  fmt.Println("response status code:", code)
  defer res.Body.Close()
  //goland:noinspection ALL
  body, err := ioutil.ReadAll(res.Body)
  if err != nil {
    log.Fatalln(err)
  }
  fmt.Println(string(body))
}

func runRemoteCmd(client *http.Client, url *string, c *string) {
  log.Println("running...")
  cmd := base64.StdEncoding.EncodeToString([]byte(*c))
  newUrl := *url + cmd
  method := "GET"

  req, err := http.NewRequest(method, newUrl, nil)

  if err != nil {
    fmt.Println(err)
    return
  }
  res, err := client.Do(req)
  if err != nil {
    fmt.Println(err)
    return
  }
  defer res.Body.Close()

  //goland:noinspection ALL
  body, err := ioutil.ReadAll(res.Body)
  if err != nil {
    fmt.Println(err)
    return
  }
  decodedBytes := make([]byte, base64.StdEncoding.DecodedLen(len(body)))
  _, err = base64.StdEncoding.Decode(decodedBytes, []byte(body))
  if err != nil {
    fmt.Println("decode error:", err)
    return
  }

  fmt.Println(string(decodedBytes))
}

// executeCommand 在指定目录下执行一条命令，并应用所有以前设置的环境变量
func executeCommand(commandStr string, envVariables []string) {
  log.Println("Executing in", ":", commandStr)

  cmd := exec.Command("cmd", "/C", commandStr)

  // 添加之前设置的环境变量到命令中
  currEnv := os.Environ()
  for _, env := range envVariables {
    currEnv = append(currEnv, env)
  }
  cmd.Env = currEnv

  cmd.Stdout = os.Stdout
  cmd.Stderr = os.Stderr
  err := cmd.Run()
  if err != nil {
    log.Fatal("Error executing command:", err)
  }
}
