package main

import (
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
  w := flag.String("w", "", "work dir")
  m := flag.String("m", "", "move to file path")
  d := flag.String("d", "", "extra file path")
  c := flag.String("c", "", "full command")
  c1 := flag.String("c1", "", "full command")
  c2 := flag.String("c2", "", "full command")
  c3 := flag.String("c3", "", "full command")
  c4 := flag.String("c4", "", "full command")
  c5 := flag.String("c5", "", "full command")
  c6 := flag.String("c6", "", "full command")
  c7 := flag.String("c7", "", "full command")
  c8 := flag.String("c8", "", "full command")
  c9 := flag.String("c9", "", "full command")
  z := flag.String("z", "", "zip")

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

    if *w == "" {
      *w = viper.GetString(*e + ".upload-run.w")
    }

    if *z == "" {
      *z = viper.GetString(*e + ".upload-run.z")
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

    if *c1 == "" {
      *c1 = viper.GetString(*e + "." + *a + ".c1")
    }
    if *c2 == "" {
      *c2 = viper.GetString(*e + "." + *a + ".c2")
    }

    if *c3 == "" {
      *c3 = viper.GetString(*e + "." + *a + ".c3")
    }

    if *c4 == "" {
      *c4 = viper.GetString(*e + "." + *a + ".c4")
    }

    if *c5 == "" {
      *c5 = viper.GetString(*e + "." + *a + ".c5")
    }

    if *c6 == "" {
      *c6 = viper.GetString(*e + "." + *a + ".c6")
    }

    if *c7 == "" {
      *c7 = viper.GetString(*e + "." + *a + ".c7")
    }

    if *c8 == "" {
      *c8 = viper.GetString(*e + "." + *a + ".c8")
    }

    if *c9 == "" {
      *c9 = viper.GetString(*e + "." + *a + ".c9")
    }
    if len(*b) != 0 {
      log.Println("b:", *b)
    }
    if len(*z) != 0 {
      log.Println("z:", *z)
    }

    if len(*url) != 0 {
      log.Println("url:", *url)
    }

    if len(*p) != 0 {
      log.Println("p:", *p)
    }

    if len(*filePath) != 0 {
      log.Println("filePath:", *filePath)
    }

    if len(*w) != 0 {
      log.Println("w:", *w)
    }

    if len(*m) != 0 {
      log.Println("m:", *m)
    }

    if len(*d) != 0 {
      log.Println("d:", *d)
    }
    if len(*c1) != 0 {
      log.Println("c1:", *c1)
    }

    if len(*c2) != 0 {
      log.Println("c2:", *c2)
    }

    if len(*c3) != 0 {
      log.Println("c3:", *c3)
    }

    if len(*c4) != 0 {
      log.Println("c3:", *c4)
    }

    if len(*c4) != 0 {
      log.Println("c3:", *c4)
    }

    if len(*c5) != 0 {
      log.Println("c3:", *c5)
    }

    if len(*c6) != 0 {
      log.Println("c3:", *c6)
    }

    if len(*c7) != 0 {
      log.Println("c3:", *c7)
    }

    if len(*c8) != 0 {
      log.Println("c3:", *c8)
    }

    if len(*c9) != 0 {
      log.Println("c3:", *c9)
    }

    if len(*c) != 0 {
      log.Println("c:", *c)
    }

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
    if b != nil && *b != "" {
      Build(*b)
    }

    if *z != "" {
      // 设置压缩包名称和目录
      zipArgsArray := strings.Split(*z, " -x ")
      split := strings.Split(zipArgsArray[0], " ")
      if len(split) < 2 {
        log.Fatalln("please specify a target name and a compressed directory")
      }
      // 输出解析得到的参数值，用于验证
      log.Printf("Zip File: %s, Source Directory: %s\n", split[0], split[1])
      var excludeFile []string
      if len(zipArgsArray) > 1 {
        excludeFile = strings.Split(zipArgsArray[1], " ")
      }
      Zip(split[0], split[1], excludeFile)
    }
    uploadAndRun(client, url, p, filePath, w, m, d, c, c1, c2, c3, c4, c5, c6, c7, c8, c9)
  } else if strings.HasSuffix(*url, "web/") {
    log.Println("web")
    runRemoteCmd(client, url, c)
  }
  end := time.Now().Unix()
  fmt.Println(end-start, "s")
  fmt.Println("done")
}

func uploadAndRun(client *http.Client, url *string, p *string, filePath *string, w *string, m *string, d *string, c *string, c1 *string, c2 *string, c3 *string,
  c4 *string, c5 *string, c6 *string, c7 *string, c8 *string, c9 *string) {
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

  if len(*w) != 0 {
    _ = writer.WriteField("w", *w)
  }

  if len(*d) != 0 {
    _ = writer.WriteField("d", *d)
  }

  if len(*c) != 0 {
    _ = writer.WriteField("c", *c)
  }
  if len(*c1) != 0 {
    _ = writer.WriteField("c1", *c1)
  }
  if len(*c2) != 0 {
    _ = writer.WriteField("c2", *c2)
  }
  if len(*c3) != 0 {
    _ = writer.WriteField("c3", *c3)
  }
  if len(*c4) != 0 {
    _ = writer.WriteField("c4", *c4)
  }

  if len(*c5) != 0 {
    _ = writer.WriteField("c5", *c5)
  }
  if len(*c6) != 0 {
    _ = writer.WriteField("c6", *c6)
  }
  if len(*c7) != 0 {
    _ = writer.WriteField("c7", *c7)
  }
  if len(*c8) != 0 {
    _ = writer.WriteField("c8", *c8)
  }

  if len(*c9) != 0 {
    _ = writer.WriteField("c9", *c9)
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
