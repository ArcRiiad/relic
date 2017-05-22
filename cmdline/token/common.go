/*
 * Copyright (c) SAS Institute Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package token

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"gerrit-pdt.unx.sas.com/tools/relic.git/cmdline/shared"
	"gerrit-pdt.unx.sas.com/tools/relic.git/config"
	"gerrit-pdt.unx.sas.com/tools/relic.git/lib/passprompt"
	"gerrit-pdt.unx.sas.com/tools/relic.git/signers/sigerrors"
	"gerrit-pdt.unx.sas.com/tools/relic.git/token"
	"gerrit-pdt.unx.sas.com/tools/relic.git/token/p11token"
	"github.com/spf13/cobra"
)

var (
	argFile      string
	argKeyName   string
	argToken     string
	argLabel     string
	argTokenName string
	argRsaBits   uint
	argEcdsaBits uint
)

var tokenMap map[string]token.Token

func addSelectOrGenerateFlags(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&argKeyName, "key", "k", "", "Name of key section in config file to use")
	cmd.Flags().StringVarP(&argToken, "token", "t", "", "Name of token to generate key in")
	cmd.Flags().StringVarP(&argLabel, "label", "l", "", "Label to attach to generated key")
	cmd.Flags().UintVar(&argRsaBits, "generate-rsa", 0, "Generate a RSA key of the specified bit size, if needed")
	cmd.Flags().UintVar(&argEcdsaBits, "generate-ecdsa", 0, "Generate an ECDSA key of the specified curve size, if needed")
}

// Update key config with values from --token and --label
func newKeyConfig() (*config.KeyConfig, error) {
	if err := shared.InitConfig(); err != nil {
		return nil, err
	}
	var keyConf *config.KeyConfig
	if argKeyName != "" {
		var err error
		keyConf, err = shared.CurrentConfig.GetKey(argKeyName)
		if err != nil {
			return nil, err
		}
	} else {
		if argToken == "" || argLabel == "" {
			return nil, errors.New("Either --key, or --token and --label, must be set")
		}
		argKeyName = fmt.Sprintf("new-key-%d", time.Now().UnixNano())
		keyConf = shared.CurrentConfig.NewKey(argKeyName)
	}
	if argToken != "" {
		tokenConf, err := shared.CurrentConfig.GetToken(argToken)
		if err != nil {
			return nil, err
		}
		keyConf.SetToken(tokenConf)
	}
	if argLabel != "" {
		keyConf.Label = argLabel
		keyConf.ID = ""
	}
	return keyConf, nil
}

func selectOrGenerate() (key token.Key, err error) {
	keyConf, err := newKeyConfig()
	if err != nil {
		return nil, err
	}
	tok, err := openToken(keyConf.Token)
	if err != nil {
		return nil, err
	}
	key, err = tok.GetKey(argKeyName)
	if err == nil {
		fmt.Fprintln(os.Stderr, "Using existing key in token")
		return key, nil
	} else if _, ok := err.(sigerrors.KeyNotFoundError); !ok {
		return nil, err
	}
	fmt.Fprintln(os.Stderr, "Generating a new key in token")
	if argRsaBits != 0 {
		return tok.Generate(argKeyName, token.KeyTypeRsa, argRsaBits)
	} else if argEcdsaBits != 0 {
		return tok.Generate(argKeyName, token.KeyTypeEcdsa, argEcdsaBits)
	} else {
		return nil, errors.New("No matching key exists, specify --generate-rsa or --generate-ecdsa to generate one")
	}
}

func openToken(tokenName string) (token.Token, error) {
	tok, ok := tokenMap[tokenName]
	if ok {
		return tok, nil
	}
	err := shared.InitConfig()
	if err != nil {
		return nil, err
	}
	var prompt passprompt.PasswordGetter
	if !argServer {
		prompt = new(passprompt.PasswordPrompt)
	}
	cfg := shared.CurrentConfig
	tcfg, err := cfg.GetToken(tokenName)
	if err != nil {
		return nil, err
	}
	switch tcfg.Type {
	case "pkcs11", "":
		tok, err = p11token.Open(cfg, tokenName, prompt)
	//case "file":
	//	tok, err = filetoken.Open(tcfg, prompt)
	default:
		return nil, fmt.Errorf("unknown token type %s", tcfg.Type)
	}
	if err != nil {
		return nil, err
	}
	if tokenMap == nil {
		tokenMap = make(map[string]token.Token)
	}
	tokenMap[tokenName] = tok
	return tok, nil
}

func openTokenByKey(keyName string) (token.Token, error) {
	if keyName == "" {
		return nil, errors.New("--key is a required parameter")
	}
	err := shared.InitConfig()
	if err != nil {
		return nil, err
	}
	keyConf, err := shared.CurrentConfig.GetKey(keyName)
	if err != nil {
		return nil, err
	}
	tok, err := openToken(keyConf.Token)
	if err != nil {
		return nil, err
	}
	return tok, nil
}

func openKey(keyName string) (token.Key, error) {
	tok, err := openTokenByKey(keyName)
	if err != nil {
		return nil, err
	}
	key, err := tok.GetKey(keyName)
	if err != nil {
		tok.Close()
		return nil, err
	}
	return key, err
}

func formatKeyID(keyID []byte) string {
	chunks := make([]string, len(keyID))
	for i, j := range keyID {
		chunks[i] = fmt.Sprintf("%02x", j)
	}
	return strings.Join(chunks, ":")
}
