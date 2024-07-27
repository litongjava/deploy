package tests

import (
  "fmt"
  "strings"
  "testing"
)

func TestSplit(test *testing.T) {
  var line = ". . -x .git node_modules"
  split := strings.Split(line, " -x ")
  for i := range split {
    fmt.Println(split[i])
  }
}
