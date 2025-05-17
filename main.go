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
	"unicode/utf8"

	"github.com/go-git/go-git/plumbing/format/gitignore"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/unicode"
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

// getEncodingByName returns an encoding.Encoding by name
func getEncodingByName(name string) (encoding.Encoding, error) {
	switch strings.ToLower(name) {
	case "shift-jis", "shiftjis", "sjis":
		return japanese.ShiftJIS, nil
	case "euc-jp", "eucjp":
		return japanese.EUCJP, nil
	case "iso-2022-jp", "iso2022jp":
		return japanese.ISO2022JP, nil
	case "utf-16le":
		return unicode.UTF16(unicode.LittleEndian, unicode.UseBOM), nil
	case "utf-16be":
		return unicode.UTF16(unicode.BigEndian, unicode.UseBOM), nil
	case "utf-8", "utf8":
		return unicode.UTF8, nil
	default:
		return nil, fmt.Errorf("サポートされていないエンコーディング: %s", name)
	}
}

// detectAndConvertEncoding attempts to detect the encoding of the given data and convert it to UTF-8
func detectAndConvertEncoding(data []byte, encodingName string) (string, error) {
	// If encoding is specified, use that
	if encodingName != "" {
		enc, err := getEncodingByName(encodingName)
		if err != nil {
			return "", fmt.Errorf("指定されたエンコーディング '%s' が見つかりません: %v", encodingName, err)
		}
		decoder := enc.NewDecoder()
		result, err := decoder.Bytes(data)
		if err != nil {
			return "", fmt.Errorf("指定されたエンコーディング '%s' でデコードできませんでした: %v", encodingName, err)
		}
		return string(result), nil
	}

	// First check if it's already valid UTF-8
	if utf8.Valid(data) {
		return string(data), nil
	}

	// Try common encodings
	encodings := []encoding.Encoding{
		japanese.ShiftJIS,
		japanese.EUCJP,
		japanese.ISO2022JP,
		unicode.UTF16(unicode.LittleEndian, unicode.UseBOM),
		unicode.UTF16(unicode.BigEndian, unicode.UseBOM),
	}

	for _, enc := range encodings {
		decoder := enc.NewDecoder()
		result, err := decoder.Bytes(data)
		if err == nil && utf8.Valid(result) {
			return string(result), nil
		}
	}

	// If we can't determine the encoding, just return as is with a warning
	fmt.Fprintf(os.Stderr, "警告: ファイルのエンコーディングを検出できませんでした。UTF-8として処理します。\n")
	return string(data), nil
}

// ディレクトリ内のテキストファイルの内容を収集
func collectFilesContent(absInputPath string, targetExtensions []string, matcher gitignore.Matcher, encodingName string) (map[string]string, error) {
	filesContent := make(map[string]string)

	err := filepath.Walk(absInputPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Fprintf(os.Stderr, "パスへのアクセスエラー: %v\n", err)
			return nil
		}

		// 相対パスを取得して matcher で判定
		relPath, err := filepath.Rel(absInputPath, path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "相対パス取得エラー: %v\n", err)
			return nil
		}

		// 隠しディレクトリ or 隠しファイルは無視（ただし .gitignore は例外）
		if strings.HasPrefix(info.Name(), ".") {
			// ディレクトリを無視する場合は SkipDir を返す
			if info.IsDir() {
				return filepath.SkipDir
			}
			// ファイルの場合も .gitignore 以外は無視
			if info.Name() != ".gitignore" {
				return nil
			}
		}

		// matcher で無視判定
		// (隠しファイル/ディレクトリは上記で既にスキップ済み)
		if matcher != nil && matcher.Match(strings.Split(relPath, string(os.PathSeparator)), info.IsDir()) {
			// ディレクトリなら以降探索をスキップ
			if info.IsDir() {
				return filepath.SkipDir
			}
			// ファイルなら無視
			return nil
		}

		// ディレクトリは継続
		if info.IsDir() {
			return nil
		}

		// もし拡張子リストに "." が含まれていたら、すべてのファイルを対象
		// 含まれていなければ、通常通り拡張子チェックを行う
		if !slices.Contains(targetExtensions, ".") {
			if !slices.Contains(targetExtensions, filepath.Ext(path)) {
				return nil
			}
		}

		// ファイル内容を読み取る
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ファイル読み取りエラー: %v\n", err)
			return nil
		}

		// エンコーディング検出と変換
		content, err := detectAndConvertEncoding(data, encodingName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "エンコーディング変換エラー (%s): %v\n", path, err)
			return nil
		}
		filesContent[path] = content

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
	encodingName := flag.String("encoding", "", "入力ファイルのエンコーディング（例：shift-jis, euc-jp, iso-2022-jp）。指定がなければ自動検出を試みます。")
	showHelp := flag.Bool("h", false, "ヘルプメッセージを表示")
	flag.Parse()

	if *showHelp {
		printHelp()
		return
	}

	// デフォルト拡張子を設定 (.py)。ただし -e . と指定された場合は全ファイル対象。
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

	filesContent, err := collectFilesContent(absInputPath, exts, matcher, *encodingName)
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