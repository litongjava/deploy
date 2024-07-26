package main

import (
  "archive/zip"
  "bufio"
  "bytes"
  "crypto/tls"
  "encoding/base64"
  "flag"
  "fmt"
  "github.com/spf13/viper"
  "golang.org/x/text/encoding/simplifiedchinese"
  "golang.org/x/text/transform"
  "io"
  "io/ioutil"
  "log"
  "mime/multipart"
  "net/http"
  "os"
  "os/exec"
  "path/filepath"
  "regexp"
  "runtime"
  "strings"
  "time"
  "unicode"
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
  c1 := flag.String("c1", "", "full command")
  c2 := flag.String("c2", "", "full command")
  c3 := flag.String("c3", "", "full command")
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

    log.Println("b:", *b)
    log.Println("z:", *z)
    log.Println("url:", *url)
    log.Println("p:", *p)
    log.Println("filePath:", *filePath)
    log.Println("m:", *m)
    log.Println("d:", *d)
    log.Println("c1:", *c1)
    log.Println("c2:", *c2)
    log.Println("c3:", *c3)
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
    build(*b)

    if *z != "" {
      // 设置压缩包名称和目录
      split := strings.Split(*z, " ")
      if len(split) < 2 {
        log.Fatalln("please specify a target name and a compressed directory")
      }
      // 输出解析得到的参数值，用于验证
      log.Printf("Zip File: %s, Source Directory: %s\n", split[0], split[1])
      err := Zip(split[0], split[1], nil)
      if err != nil {
        log.Fatalln(err)
      }
    }
    uploadAndRun(client, url, p, filePath, m, d, c, c1, c2, c3)
  } else if strings.HasSuffix(*url, "web/") {
    log.Println("web")
    runRemoteCmd(client, url, c)
  }
  end := time.Now().Unix()
  fmt.Println(end-start, "s")
  fmt.Println("done")
}

func uploadAndRun(client *http.Client, url *string, p *string, filePath *string, m *string, d *string, c *string, c1 *string, c2 *string, c3 *string) {
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
  if len(*c1) != 0 {
    _ = writer.WriteField("c1", *c1)
  }
  if len(*c2) != 0 {
    _ = writer.WriteField("c2", *c2)
  }
  if len(*c3) != 0 {
    _ = writer.WriteField("c3", *c3)
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

func build(filePath string) {
  file, err := os.Open(filePath)
  if err != nil {
    log.Fatalln("Error opening file:", err)
  }
  defer file.Close()

  envSection := ""
  buildSection := ""

  switch runtime.GOOS {
  case "windows":
    envSection = "[win.env]"
    buildSection = "[win.build]"
  case "darwin":
    envSection = "[mac.env]"
    buildSection = "[mac.build]"
  case "linux":
    envSection = "[linux.env]"
    buildSection = "[linux.build]"
  default:
    log.Fatalln("not support current os")
    return
  }

  envVariables := []string{}
  buildCommands := []string{}

  scanner := bufio.NewScanner(file)
  section := ""

  for scanner.Scan() {
    line := scanner.Text()
    line = strings.TrimSpace(line)

    // 跳过注释行
    if strings.HasPrefix(line, "#") || line == "" {
      continue
    }

    // 判断当前行是否是一个新的部分
    if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
      section = line
      continue
    }

    // 根据当前部分判断如何处理当前行
    if section == envSection {
      if strings.HasPrefix(line, "set ") || strings.HasPrefix(line, "export ") {
        value := strings.TrimSpace(line)
        envVariables = append(envVariables, value)
        log.Println("add env variable:", value)
      }
    } else if section == buildSection {
      buildCommands = append(buildCommands, line)
    }
  }

  if err := scanner.Err(); err != nil {
    log.Fatalln("Error reading file:", err)
  }

  // 设置环境变量
  for _, env := range envVariables {
    keyValue := strings.SplitN(env, "=", 2)
    if len(keyValue) == 2 {
      os.Setenv(keyValue[0], keyValue[1])
    }
  }

  // 执行构建命令
  for _, command := range buildCommands {
    executeCommand(command, envVariables)
  }
}

// executeCommand 在指定目录下执行一条命令，并应用所有以前设置的环境变量
func executeCommand(commandStr string, envVariables []string) {
  log.Println("Executing in", ":", commandStr)

  cmd := exec.Command("cmd", "/C", commandStr)
  if runtime.GOOS != "windows" {
    cmd = exec.Command("sh", "-c", commandStr)
  }

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

func Zip(target string, sourcePath string, excludeFile *string) error {
  zipfile, err := os.Create(target)
  if err != nil {
    return err
  }
  defer zipfile.Close()

  archive := zip.NewWriter(zipfile)
  defer archive.Close()

  info, err := os.Stat(sourcePath)
  if err != nil {
    return err
  }

  var base string
  if info.IsDir() {
    base = filepath.Base(sourcePath)
  } else {
    base = filepath.Base(filepath.Dir(sourcePath))
  }

  if info.IsDir() {
    err = filepath.Walk(sourcePath, func(path string, info os.FileInfo, err error) error {
      return addFileToZip(archive, path, sourcePath, base, excludeFile, info)
    })
  } else {
    err = addFileToZip(archive, sourcePath, filepath.Dir(sourcePath), base, excludeFile, info)
  }

  return err
}

func addFileToZip(archive *zip.Writer, path, sourcePath, base string, excludeFile *string, info os.FileInfo) error {
  if info.IsDir() && path == sourcePath {
    return nil
  }

  // Get relative path
  relPath, err := filepath.Rel(sourcePath, path)
  if err != nil {
    return err
  }

  if excludeFile != nil && matchExcludeFile(relPath, *excludeFile) {
    return nil // Skip files matching the exclude pattern
  }

  header, err := zip.FileInfoHeader(info)
  if err != nil {
    return err
  }

  if strings.Contains(relPath, string(os.PathSeparator)) {
    relPath = strings.Replace(relPath, string(os.PathSeparator), "/", -1)
  }

  // Handle Chinese encoding
  if IsChineseChar(relPath) {
    relPath = GetChineseName(relPath)
  }

  header.Name = base + "/" + relPath

  if info.IsDir() {
    header.Name += "/"
  } else {
    header.Method = zip.Deflate
  }

  writer, err := archive.CreateHeader(header)
  if err != nil {
    return err
  }

  if info.IsDir() {
    return nil
  }

  file, err := os.Open(path)
  if err != nil {
    return err
  }
  defer file.Close()

  _, err = io.Copy(writer, file)
  return err
}

// matchExcludeFile checks if the filename matches the exclude pattern
func matchExcludeFile(filename string, pattern string) bool {
  matched, err := filepath.Match(pattern, filename)
  if err != nil {
    log.Printf("Error matching file with pattern: %v", err)
    return false
  }
  return matched
}

// IsChineseChar checks if the string contains Chinese characters
func IsChineseChar(str string) bool {
  for _, r := range str {
    compile := regexp.MustCompile("[\u3002\uff1b\uff0c\uff1a\u201c\u201d\uff08\uff09\u3001\uff1f\u300a\u300b]")
    if unicode.Is(unicode.Scripts["Han"], r) || compile.MatchString(string(r)) {
      return true
    }
  }
  return false
}

// GetChineseName encodes the filename in GB18030
func GetChineseName(filename string) string {
  reader := bytes.NewReader([]byte(filename))
  encoder := transform.NewReader(reader, simplifiedchinese.GB18030.NewEncoder())
  content, _ := ioutil.ReadAll(encoder)
  return string(content)
}
