package push

import (
	"fmt"

	webpush "github.com/SherClockHolmes/webpush-go"
)

func GenerateVAPIDKeys() (publicKey, privateKey string, err error) {
	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return "", "", fmt.Errorf("generate vapid keys: %w", err)
	}
	return pub, priv, nil
}
