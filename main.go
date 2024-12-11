package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-git/go-git/plumbing/format/gitignore"
)

type extensionsFlag []string

func (e *extensionsFlag) String() string {
	return strings.Join(*e, ", ")
}

func (e *extensionsFlag) Set(value string) error {
	parts := strings.Split(value, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if !strings.HasPrefix(p, ".") {
			p = "." + p
		}
		*e = append(*e, p)
	}
	return nil
}

func printHelp() {
	fmt.Println("Usage options:")
	flag.PrintDefaults()
}

func exitWithError(message string) {
	fmt.Fprintln(os.Stderr, "エラー:", message)
	os.Exit(1)
}

// ディレクトリ内のテキストファイルの内容を収集
func collectFilesContent(absInputPath string, targetExtensions []string, matcher gitignore.Matcher) (map[string]string, error) {
	filesContent := make(map[string]string)

	err := filepath.Walk(absInputPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Fprintf(os.Stderr, "パスへのアクセスエラー: %v\n", err)
			return nil
		}

		// 相対パスを取得してmatcherで判定
		relPath, err := filepath.Rel(absInputPath, path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "相対パス取得エラー: %v\n", err)
			return nil
		}

		// matcherで無視判定
		if matcher != nil && matcher.Match(strings.Split(relPath, string(os.PathSeparator)), info.IsDir()) {
			// ディレクトリなら以降探索をスキップ
			if info.IsDir() {
				return filepath.SkipDir
			}
			// ファイルならこのファイルは無視
			return nil
		}

		// 隠しファイルを無視する
		if strings.HasPrefix(info.Name(), ".") && info.Name() != ".gitignore" {
			return nil
		}

		// ディレクトリは継続
		if info.IsDir() {
			return nil
		}

		// 拡張子が一致しなければスキップ
		if !slices.Contains(targetExtensions, filepath.Ext(path)) {
			return nil
		}

		// ファイル内容を読み取る
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ファイル読み取りエラー: %v\n", err)
			return nil
		}
		filesContent[path] = string(data)

		return nil
	})

	return filesContent, err
}

// createPrompt はリポジトリのファイル内容と指示文を組み合わせたプロンプトを生成します。
func createPrompt(filesContent map[string]string, instructions string) string {
	var promptBuilder strings.Builder

	promptBuilder.WriteString("以下は対象リポジトリのすべてのファイル内容です。\n")
	promptBuilder.WriteString("これらを参考に、後述の指示に従ってリポジトリを変更してください。\n\n")

	for path, content := range filesContent {
		promptBuilder.WriteString(fmt.Sprintf("----------\n[File]: %s\n[Content Start]\n", path))
		promptBuilder.WriteString(content)
		promptBuilder.WriteString("\n[Content End]\n\n")
	}

	promptBuilder.WriteString("----------\n以下が指示文です:\n")
	promptBuilder.WriteString(instructions)

	return promptBuilder.String()
}

func loadGitignorePatterns(gitignorePath string) (gitignore.Matcher, error) {
	// .gitignoreがない場合はnilを返す
	if _, err := os.Stat(gitignorePath); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	file, err := os.Open(gitignorePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	ps := make([]gitignore.Pattern, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		// 空行やコメント行はスキップ
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		ps = append(ps, gitignore.ParsePattern(line, nil))
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return gitignore.NewMatcher(ps), nil
}

func main() {
	var exts extensionsFlag
	flag.Var(&exts, "e", "対象の拡張子（例：-e .py -e .go あるいは -e .py,.go）")
	inputPath := flag.String("p", "./", "入力ディレクトリのパス (絶対パスまたは相対パス)")
	showHelp := flag.Bool("h", false, "ヘルプメッセージを表示")
	flag.Parse()

	if *showHelp {
		printHelp()
		return
	}

	if len(exts) == 0 {
		exts = []string{".py"}
	}

	absInputPath, err := filepath.Abs(*inputPath)
	if err != nil {
		exitWithError(fmt.Sprintf("入力パスの解析に失敗しました: %v", err))
	}

	// .gitignoreを読み込み
	matcher, err := loadGitignorePatterns(filepath.Join(absInputPath, ".gitignore"))
	if err != nil {
		exitWithError(fmt.Sprintf(".gitignoreの読み込みに失敗しました: %v", err))
	}

	filesContent, err := collectFilesContent(absInputPath, exts, matcher)
	if err != nil {
		exitWithError(fmt.Sprintf("ファイル内容の収集中にエラーが発生しました: %v", err))
	}

	if len(filesContent) == 0 {
		exitWithError("有効なファイルが見つかりませんでした")
	}

	fmt.Println("変更の指示文を入力してください（Ctrl+Dで終了）:")
	scanner := bufio.NewScanner(os.Stdin)
	var instructions bytes.Buffer
	for scanner.Scan() {
		instructions.WriteString(scanner.Text())
		instructions.WriteString("\n")
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "標準入力読み取りエラー: %v\n", err)
		os.Exit(1)
	}

	finalPrompt := createPrompt(filesContent, instructions.String())
	fmt.Println(finalPrompt)
}
