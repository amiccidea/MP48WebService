package main

import (
	"errors"
	"regexp"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

var qwertySequences = []string{
	"1234567890",
	"qwertyuiop",
	"asdfghjkl",
	"zxcvbnm",
	"QWERTYUIOP",
	"ASDFGHJKL",
	"ZXCVBNM",
}

var forbiddenSequences = func() []string {
	var seqs []string
	for _, row := range qwertySequences {
		rowLen := len(row)
		for i := 0; i < rowLen; i++ {
			for j := i + 4; j <= rowLen; j++ {
				seqs = append(seqs, row[i:j])
			}
		}
		rev := reverseString(row)
		for i := 0; i < rowLen; i++ {
			for j := i + 4; j <= rowLen; j++ {
				seqs = append(seqs, rev[i:j])
			}
		}
	}
	return seqs
}()

func reverseString(s string) string {
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

func hasThreeConsecutiveSameChars(s string) bool {
	for i := 0; i < len(s)-2; i++ {
		if s[i] == s[i+1] && s[i+1] == s[i+2] {
			return true
		}
	}
	return false
}

func containsQwertySequence(s string) bool {
	for _, seq := range forbiddenSequences {
		if strings.Contains(s, seq) {
			return true
		}
	}
	return false
}

func validatePasswordComplexity(password string) error {
	if len(password) < 12 {
		return errors.New("La password deve avere almeno 12 caratteri")
	}
	if !regexp.MustCompile(`[A-Za-z]`).MatchString(password) {
		return errors.New("La password deve contenere almeno una lettera")
	}
	if !regexp.MustCompile(`[0-9]`).MatchString(password) {
		return errors.New("La password deve contenere almeno un numero")
	}
	if !regexp.MustCompile(`[^A-Za-z0-9]`).MatchString(password) {
		return errors.New("La password deve contenere almeno un carattere speciale")
	}
	if hasThreeConsecutiveSameChars(password) {
		return errors.New("La password non può contenere 3 o più caratteri uguali consecutivi")
	}
	if containsQwertySequence(password) {
		return errors.New("La password non può contenere sequenze di 4 o più caratteri consecutivi sulla tastiera QWERTY")
	}
	return nil
}

func isPasswordReused(user *User, newPassword string) bool {
	for _, oldHash := range user.PasswordHistory {
		if err := bcrypt.CompareHashAndPassword([]byte(oldHash), []byte(newPassword)); err == nil {
			return true
		}
	}
	return false
}

func updatePasswordHistory(user *User, oldHash string) {
	history := append([]string{oldHash}, user.PasswordHistory...)
	if len(history) > 5 {
		history = history[:5]
	}
	user.PasswordHistory = history
}

func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}
