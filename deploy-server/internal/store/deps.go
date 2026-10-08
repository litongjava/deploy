package store

import (
	"encoding/json"
	"fmt"
	"strings"
)

// 依赖构建的领域逻辑：依赖是什么、能不能构建、按什么顺序构建。
//
// 依赖用「项目 key」表达（与 CLI / Hook / URL 引用项目的方式一致），
// 存在 Project.Dependencies 这个 JSON 数组列里，声明顺序即同一层内的先后顺序。

// ParseDependencies 解析依赖 key 列表；坏数据按空处理，不让它挡住构建。
func ParseDependencies(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var keys []string
	if err := json.Unmarshal([]byte(raw), &keys); err != nil {
		return nil
	}
	return NormalizeKeys(keys)
}

// NormalizeKeys 去掉空白、空串与重复项，保持首次出现的顺序。
func NormalizeKeys(keys []string) []string {
	out := make([]string, 0, len(keys))
	seen := map[string]bool{}
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	return out
}

// EncodeDependencies 序列化依赖 key 列表，空列表存空串。
func EncodeDependencies(keys []string) string {
	keys = NormalizeKeys(keys)
	if len(keys) == 0 {
		return ""
	}
	b, err := json.Marshal(keys)
	if err != nil {
		return ""
	}
	return string(b)
}

// ResolveDependencies 返回构建 p 之前必须先构建的项目，顺序即构建顺序。
//
// 规则（与需求规格说明 §5.2 的依赖构建一致）：
//   - 深度优先、依赖在前、同一个项目只出现一次（菱形依赖只构建一次）
//   - 依赖可以是任何未归档的项目（应用项目与构建项目都可以）；构建项目只是
//     「通常是产出产物的底层库」的约定，不是能不能被依赖的开关
//   - 依赖不存在 / 已归档 / 成环都直接报错，绝不静默跳过
func (s *Store) ResolveDependencies(p *Project) ([]Project, error) {
	out := make([]Project, 0, 4)
	visiting := map[uint]bool{p.ID: true} // 根节点入栈，便于发现绕回自己的环
	done := map[uint]bool{}

	var walk func(cur *Project, path []string) error
	walk = func(cur *Project, path []string) error {
		for _, key := range ParseDependencies(cur.Dependencies) {
			var d Project
			if err := s.db.Where("key = ?", key).First(&d).Error; err != nil {
				return fmt.Errorf("dependency %q of %s does not exist", key, cur.Key)
			}
			if d.Archived {
				return fmt.Errorf("dependency %s is archived; unarchive it or remove it from the dependency list", d.Key)
			}
			if visiting[d.ID] {
				return fmt.Errorf("dependency cycle detected: %s", strings.Join(append(path, d.Key), " -> "))
			}
			if done[d.ID] {
				continue
			}
			visiting[d.ID] = true
			if err := walk(&d, append(path, d.Key)); err != nil {
				return err
			}
			visiting[d.ID] = false
			done[d.ID] = true
			out = append(out, d)
		}
		return nil
	}
	if err := walk(p, []string{p.Key}); err != nil {
		return nil, err
	}
	return out, nil
}

// DependencyGraph 返回 key -> 直接依赖列表的全量图，供保存前的环检测使用。
func (s *Store) DependencyGraph() (map[string][]string, error) {
	var projects []Project
	if err := s.db.Select("key", "dependencies").Find(&projects).Error; err != nil {
		return nil, err
	}
	g := make(map[string][]string, len(projects))
	for _, p := range projects {
		g[p.Key] = ParseDependencies(p.Dependencies)
	}
	return g, nil
}

// ValidateDependencyTargets 校验一份「将要保存」的依赖列表。
//
// graph 是保存前的全量图；selfKey 是被编辑项目的 key（新建时传新 key，
// 此时它还没有库内记录，环检测会把它当成一个新节点）。返回空串表示通过。
func ValidateDependencyTargets(graph map[string][]string, selfKey string, keys []string) string {
	keys = NormalizeKeys(keys)
	for _, k := range keys {
		if k == selfKey {
			return "a project cannot depend on itself: " + k
		}
		if _, ok := graph[k]; !ok {
			return "dependency does not exist: " + k
		}
	}
	// 用「新边生效后」的图检测环：从自己出发能否绕回自己。
	next := func(key string) []string {
		if key == selfKey {
			return keys
		}
		return graph[key]
	}
	stack := map[string]bool{selfKey: true}
	var walk func(string) string
	walk = func(cur string) string {
		for _, n := range next(cur) {
			if n == selfKey {
				return "dependency cycle detected: " + selfKey + " -> " + cur + " -> " + n
			}
			if stack[n] {
				return "dependency cycle detected at " + n
			}
			stack[n] = true
			if msg := walk(n); msg != "" {
				return msg
			}
			stack[n] = false
		}
		return ""
	}
	return walk(selfKey)
}
