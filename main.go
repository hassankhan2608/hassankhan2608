package main

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const userName = "hassankhan2608"
const cacheFile = "cache.json"
const maxConcurrency = 8

type githubUser struct {
	Login      string `json:"login"`
	CreatedAt  string `json:"created_at"`
	Followers  int    `json:"followers"`
	Following  int    `json:"following"`
	PublicRepos int   `json:"public_repos"`
}

type repo struct {
	Name            string `json:"name"`
	StargazersCount int    `json:"stargazers_count"`
	Fork            bool   `json:"fork"`
	Language        string `json:"language"`
	PushedAt        string `json:"pushed_at"`
}

type searchResult struct {
	TotalCount int `json:"total_count"`
}

type contributor struct {
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	Weeks []struct {
		A int `json:"a"`
		D int `json:"d"`
	} `json:"weeks"`
}

type repoCache struct {
	CommitCount int `json:"commit_count"`
	Additions   int `json:"additions"`
	Deletions   int `json:"deletions"`
}

type repoResult struct {
	Name      string
	Additions int
	Deletions int
	Changed   bool
}

func fetch(url string, target any) error {
	req, _ := http.NewRequest("GET", url, nil)
	token := os.Getenv("GH_TOKEN")
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("User-Agent", "profile-gen/1.0")
	req.Header.Set("Accept", "application/vnd.github.cloak-preview+json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	return json.Unmarshal(body, target)
}

func loadCache() map[string]repoCache {
	cache := make(map[string]repoCache)
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		return cache
	}
	json.Unmarshal(data, &cache)
	return cache
}

func saveCache(cache map[string]repoCache) {
	data, _ := json.MarshalIndent(cache, "", "  ")
	os.WriteFile(cacheFile, data, 0644)
}

func fetchRepoContributors(repoName string, cache map[string]repoCache, sem chan struct{}, wg *sync.WaitGroup, results chan<- repoResult) {
	defer wg.Done()
	sem <- struct{}{}
	defer func() { <-sem }()

	var contribs []contributor
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/stats/contributors", userName, repoName)
	if err := fetch(url, &contribs); err != nil {
		results <- repoResult{Name: repoName}
		return
	}

	add, del := 0, 0
	for _, c := range contribs {
		if c.Author.Login == userName {
			for _, w := range c.Weeks {
				add += w.A
				del += w.D
			}
			break
		}
	}

	results <- repoResult{
		Name:      repoName,
		Additions: add,
		Deletions: del,
		Changed:   true,
	}
}

func main() {
	var user githubUser
	if err := fetch("https://api.github.com/users/"+userName, &user); err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching user: %v\n", err)
		os.Exit(1)
	}

	var repos []repo
	for page := 1; page <= 5; page++ {
		var pageRepos []repo
		url := fmt.Sprintf("https://api.github.com/user/repos?per_page=100&sort=updated&type=all&page=%d", page)
		if err := fetch(url, &pageRepos); err != nil || len(pageRepos) == 0 {
			break
		}
		repos = append(repos, pageRepos...)
	}

	totalStars := 0
	ownRepos := 0
	for _, r := range repos {
		totalStars += r.StargazersCount
		if !r.Fork {
			ownRepos++
		}
	}

	var commitsSearch searchResult
	fetch("https://api.github.com/search/commits?q=author:"+userName+"&per_page=1", &commitsSearch)

	var issuesSearch searchResult
	fetch("https://api.github.com/search/issues?q=author:"+userName+"+type:issue&per_page=1", &issuesSearch)

	var prsSearch searchResult
	fetch("https://api.github.com/search/issues?q=author:"+userName+"+type:pr&per_page=1", &prsSearch)

	cache := loadCache()
	totalAdd, totalDel := 0, 0
	changed := false

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrency)
	results := make(chan repoResult, len(repos))

	for _, r := range repos {
		cached, ok := cache[r.Name]
		if ok && cached.CommitCount > 0 {
			totalAdd += cached.Additions
			totalDel += cached.Deletions
			continue
		}
		wg.Add(1)
		go fetchRepoContributors(r.Name, cache, sem, &wg, results)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	for res := range results {
		if res.Changed {
			cache[res.Name] = repoCache{
				CommitCount: 1,
				Additions:   res.Additions,
				Deletions:   res.Deletions,
			}
			changed = true
		}
		totalAdd += res.Additions
		totalDel += res.Deletions
	}

	if changed {
		saveCache(cache)
	}

	stats := map[string]string{
		"repos":     fmt.Sprintf("%d", len(repos)),
		"ownRepos":  fmt.Sprintf("%d", ownRepos),
		"stars":     fmt.Sprintf("%d", totalStars),
		"followers": fmt.Sprintf("%d", user.Followers),
		"commits":   fmt.Sprintf("%d", commitsSearch.TotalCount),
		"issues":    fmt.Sprintf("%d", issuesSearch.TotalCount),
		"prs":       fmt.Sprintf("%d", prsSearch.TotalCount),
		"loc":       fmt.Sprintf("%d", totalAdd+totalDel),
		"add":       fmt.Sprintf("%d", totalAdd),
		"del":       fmt.Sprintf("%d", totalDel),
	}

	generateSVG("light_mode.svg", stats, false)
	generateSVG("dark_mode.svg", stats, true)

	fmt.Println("✓ Generated light_mode.svg")
	fmt.Println("✓ Generated dark_mode.svg")
}

func buildTypingAnimation(fieldText string, valueColor string) string {
	var b strings.Builder
	x := 467
	for i, ch := range fieldText {
		delay := fmt.Sprintf("%.2fs", float64(i)*0.07)
		b.WriteString(fmt.Sprintf(
			`  <text x="%d" y="110" fill="%s"><tspan class="val"><animate attributeName="opacity" values="0;1" dur="0.01s" fill="freeze" begin="%s"/>%s</tspan></text>
`, x, valueColor, delay, html.EscapeString(string(ch))))
		x += 9
	}
	return b.String()
}

func generateSVG(filename string, stats map[string]string, dark bool) {
	var (
		bgColor    string
		textColor  string
		keyColor   string
		valueColor string
		commentClr string
		addColor   string
		delColor   string
		asciiClr   string
	)

	if dark {
		bgColor = "#0d1117"
		textColor = "#c9d1d9"
		keyColor = "#ffa657"
		valueColor = "#a5d6ff"
		commentClr = "#8b949e"
		addColor = "#3fb950"
		delColor = "#f85149"
		asciiClr = "#7aa2f7"
	} else {
		bgColor = "#ffffff"
		textColor = "#24292f"
		keyColor = "#cf5e2c"
		valueColor = "#0550ae"
		commentClr = "#6e7781"
		addColor = "#1a7f37"
		delColor = "#cf222e"
		asciiClr = "#0550ae"
	}

	archBytes, err := os.ReadFile("arch.txt")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading arch.txt: %v\n", err)
		os.Exit(1)
	}
	arch := string(archBytes)

	fieldText := "Full Stack Developer & Data Scientist"
	typingSpans := buildTypingAnimation(fieldText, valueColor)

	svg := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<svg xmlns="http://www.w3.org/2000/svg" font-family="'JetBrains Mono','Fira Code','Cascadia Code','Courier New',monospace" width="910px" height="600px" font-size="16px">
<style>
  .key {fill: %s;}
  .val {fill: %s;}
  .add {fill: %s;}
  .del {fill: %s;}
  .cmt {fill: %s;}
  text, tspan {white-space: pre;}
</style>

<rect width="910" height="560" fill="%s" rx="15"/>

<foreignObject x="10" y="0" width="400" height="380">
  <body xmlns="http://www.w3.org/1999/xhtml">
    <pre style="font-size: 14px; line-height: 1.15; letter-spacing: 0; color: %s;">
%s    </pre>
  </body>
</foreignObject>

<text x="400" y="30" fill="%s">
  <tspan x="400" y="30">laughingman@arch</tspan>
  <tspan x="400" y="50">—————————————————————</tspan>
  <tspan x="400" y="70" class="key">Name</tspan>: <tspan class="val">Mohd Hassan Khan</tspan>
  <tspan x="400" y="90" class="key">Location</tspan>: <tspan class="val">Mumbai, India</tspan>
  <tspan x="400" y="110" class="key">Field</tspan>:
</text>
%s
<text x="400" y="130" fill="%s">
  <tspan x="400" y="130" class="key">Editor</tspan>: <tspan class="val">Neovim, VS Code, nano, Jupyter</tspan>
  <tspan x="400" y="150" class="cmt"># neofetch</tspan>
  <tspan x="400" y="170" class="key">OS</tspan>: <tspan class="val">Arch Linux x86_64</tspan>
  <tspan x="400" y="190" class="key">Shell</tspan>: <tspan class="val">zsh</tspan>
  <tspan x="400" y="210" class="key">DE</tspan>: <tspan class="val">Hyprland</tspan>
</text>

<text x="400" y="240" fill="%s">
  <tspan x="400" y="240" class="key">Languages &amp; Tools</tspan>:
  <tspan x="400" y="260">———————————————————</tspan>
  <tspan x="400" y="280" class="val">Python, R, SQL, TypeScript, TailwindCSS</tspan><tspan class="cmt"> #Langs</tspan>
  <tspan x="400" y="300" class="val">React, Next.js, Node.js, Express, GraphQL</tspan><tspan class="cmt"> #Web</tspan>
  <tspan x="400" y="320" class="val">TensorFlow, PyTorch, Scikit-learn, NLP</tspan><tspan class="cmt"> #ML</tspan>
  <tspan x="400" y="340" class="val">Pandas, NumPy, PySpark, Kafka, Airflow</tspan><tspan class="cmt"> #Data</tspan>
  <tspan x="400" y="360" class="val">PostgreSQL, MongoDB, Redis, Elasticsearch</tspan><tspan class="cmt"> #DB</tspan>
  <tspan x="400" y="380" class="val">Docker, AWS, Kubernetes, Git, Power BI</tspan><tspan class="cmt"> #Infra</tspan>
  <tspan x="400" y="400" class="val">AI, RAG, Vector Search, LangChain, LLMs</tspan><tspan class="cmt"> #AI</tspan>
</text>

<text x="20" y="430" fill="%s">
  <tspan x="20" y="430" class="key">Contact</tspan>:
  <tspan x="20" y="450">———————</tspan>
  <tspan x="20" y="470" class="key">Email</tspan>: <tspan class="val">hassankhan2608@gmail.com</tspan>
  <tspan x="20" y="490" class="key">LinkedIn</tspan>: <tspan class="val">hassankhan2608</tspan>
  <tspan x="20" y="510" class="key">X</tspan>: <tspan class="val">@hassankhan2608</tspan>
  <tspan x="20" y="530" class="key">Web</tspan>: <tspan class="val">laughingman.is-a.dev</tspan>
</text>

<text x="400" y="430" fill="%s">
  <tspan x="400" y="430" class="key">GitHub Stats</tspan>:
  <tspan x="400" y="450">————————————</tspan>
  <tspan x="400" y="470" class="key">Repos</tspan>: <tspan class="val">%s</tspan> {<tspan class="key">Own</tspan>: <tspan class="val">%s</tspan>} | <tspan class="key">Stars</tspan>: <tspan class="val">%s</tspan>
  <tspan x="400" y="490" class="key">Commits</tspan>: <tspan class="val">%s</tspan> | <tspan class="key">Issues</tspan>: <tspan class="val">%s</tspan> | <tspan class="key">PRs</tspan>: <tspan class="val">%s</tspan>
  <tspan x="400" y="510" class="key">Followers</tspan>: <tspan class="val">%s</tspan>
  <tspan x="400" y="530" class="key">Lines of Code</tspan>: <tspan class="val">%s</tspan> (<tspan class="add">%s++</tspan>, <tspan class="del">%s--</tspan>)
</text>

</svg>`,
		keyColor, valueColor, addColor, delColor, commentClr,
		bgColor,
		asciiClr, arch,
		textColor,
		typingSpans,
		textColor, textColor, textColor, textColor,
		stats["repos"], stats["ownRepos"], stats["stars"],
		stats["commits"], stats["issues"], stats["prs"],
		stats["followers"],
		stats["loc"], stats["add"], stats["del"],
	)

	os.WriteFile(filename, []byte(svg), 0644)
}
