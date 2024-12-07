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

	clipboard "golang.design/x/clipboard"
)

// 無視するディレクトリ名
var ignoreDirs = []string{}

func printHelp() {
	fmt.Println("Usage options:")
	flag.PrintDefaults()
}

func exitWithError(message string) {
	fmt.Fprintln(os.Stderr, "エラー:", message)
	os.Exit(1)
}

// 入力された拡張子リストを正規化（ドットを付ける）
func normalizeExtensions(extList string) []string {
	extensions := strings.Split(extList, ",")
	for i, ext := range extensions {
		ext = strings.TrimSpace(ext)
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		extensions[i] = ext
	}
	return extensions
}

// ディレクトリ内のテキストファイルの内容を収集
func collectFilesContent(absInputPath string, targetExtensions []string) (map[string]string, error) {
	filesContent := make(map[string]string)

	err := filepath.Walk(absInputPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Fprintf(os.Stderr, "パスへのアクセスエラー: %v\n", err)
			return nil
		}

		// 隠しファイルを無視する
		if strings.HasPrefix(info.Name(), ".") {
			return nil
		}

		// ディレクトリを無視する
		if info.IsDir() {
			if slices.Contains(ignoreDirs, info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		// 拡張子が一致するファイルを処理
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

// copyToClipboard は指定された文字列をクリップボードにコピーします。
func copyToClipboard(content string) error {
	err := clipboard.Init()
	if err != nil {
		return err
	}

	clipboard.Write(clipboard.FmtText, []byte(content))
	return nil
}

func main() {
	inputPath := flag.String("p", "./", "入力ディレクトリのパス (絶対パスまたは相対パス)")
	targetExtensions := flag.String("e", ".py", "対象の拡張子 (カンマ区切りで複数指定可能)")
	showHelp := flag.Bool("h", false, "ヘルプメッセージを表示")
	flag.Parse()

	if *showHelp {
		printHelp()
		return
	}

	// 入力ディレクトリの絶対パスを取得
	absInputPath, err := filepath.Abs(*inputPath)
	if err != nil {
		exitWithError(fmt.Sprintf("入力パスの解析に失敗しました: %v", err))
	}

	// 拡張子リストを正規化
	extensions := normalizeExtensions(*targetExtensions)

	// ディレクトリ内のファイル内容を取得
	filesContent, err := collectFilesContent(absInputPath, extensions)
	if err != nil {
		exitWithError(fmt.Sprintf("ファイル内容の収集中にエラーが発生しました: %v", err))
	}

	if len(filesContent) == 0 {
		exitWithError("有効なファイルが見つかりませんでした")
	}

	// 標準入力から指示文を読み込み
	fmt.Println("変更の指示文を入力してください（Ctrl+Dで終了）:")
	scanner := bufio.NewScanner(os.Stdin)
	var instructions bytes.Buffer
	for scanner.Scan() {
		instructions.WriteString(scanner.Text())
		instructions.WriteString("\n")
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "error reading standard input: %v\n", err)
		os.Exit(1)
	}

	finalPrompt := createPrompt(filesContent, instructions.String())

	fmt.Println(finalPrompt)
}
