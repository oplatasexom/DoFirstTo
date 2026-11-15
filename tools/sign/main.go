// sign создает ключи и подписывает сборки для обновлений
//
//	go run ./tools/sign keygen -out release.key
//	    Печатает открытый ключ (его вшивают в сборку через
//	    -X dofirst/internal/config.UpdateKey=...) и записывает закрытый
//	    ключ в файл
//
//	go run ./tools/sign sign -key release.key dist/dofirst-windows-amd64.exe
//	    Создает рядом файл подписи с расширением .sig. Закрытый ключ можно
//	    передать и через переменную окружения DOFIRST_SIGN_KEY (для CI)
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"dofirst/internal/service/update"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "sign:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: sign keygen -out FILE | sign sign [-key FILE] BINARY...")
	}
	switch args[0] {
	case "keygen":
		return keygen(args[1:])
	case "sign":
		return sign(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func keygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	out := fs.String("out", "release.key", "файл для закрытого ключа")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if _, err := os.Stat(*out); err == nil {
		return fmt.Errorf("%s already exists: refusing to overwrite a key", *out)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}
	if err := os.WriteFile(*out, []byte(base64.StdEncoding.EncodeToString(private)+"\n"), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", *out, err)
	}
	fmt.Println("Закрытый ключ записан в", *out, "— храните его вне репозитория.")
	fmt.Println("Открытый ключ для сборки (config.UpdateKey):")
	fmt.Println(base64.StdEncoding.EncodeToString(public))
	return nil
}

func sign(args []string) error {
	fs := flag.NewFlagSet("sign", flag.ContinueOnError)
	keyFile := fs.String("key", "", "файл закрытого ключа (иначе берётся DOFIRST_SIGN_KEY)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return errors.New("no files to sign")
	}

	encoded := os.Getenv("DOFIRST_SIGN_KEY")
	if *keyFile != "" {
		data, err := os.ReadFile(*keyFile)
		if err != nil {
			return fmt.Errorf("read key: %w", err)
		}
		encoded = string(data)
	}
	private, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil || len(private) != ed25519.PrivateKeySize {
		return errors.New("private key is missing or malformed")
	}

	for _, path := range fs.Args() {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		if err := os.WriteFile(path+".sig", []byte(update.Sign(private, data)+"\n"), 0o644); err != nil {
			return fmt.Errorf("write signature: %w", err)
		}
		fmt.Println(path + ".sig")
	}
	return nil
}
