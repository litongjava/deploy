package main

import (
  "bufio"
  "log"
  "os"
  "os/exec"
  "runtime"
  "strings"
)

func Build(filePath string) {
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
    ExecuteCommand(command, envVariables)
  }
}

// executeCommand 在指定目录下执行一条命令，并应用所有以前设置的环境变量
func ExecuteCommand(commandStr string, envVariables []string) {
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
