package auth

import (
	"errors"
	"fmt"
	"math/rand"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"use-gin/config"
	"use-gin/errcode"
	"use-gin/handler"
)

type PublicParamsDebug struct {
	KeyID string `form:"KeyID" binding:"required"`
}

type PublicParams struct {
	PublicParamsDebug
	Timestamp time.Time `form:"Timestamp" binding:"required" time_format:"unix"`
	Nonce     int       `form:"Nonce" binding:"required"`
	Signature string    `form:"Signature" binding:"required"`
}

func VerifySign(c *gin.Context, signType string) (map[string]string, error) {
	debug := c.Query("debug")

	if err := c.Request.ParseForm(); err != nil {
		err1 := errcode.New(errcode.InternalServerError, err)
		handler.SendResponse(c, err1, nil)
		c.Abort()
		return nil, nil
	}
	allParamsMap := c.Request.Form

	var paramsDebug PublicParamsDebug
	var params PublicParams

	keyID := ""
	if debug == "1" {
		if err := c.ShouldBind(&paramsDebug); err != nil {
			err1 := errcode.New(errcode.ValidationError, err)
			err1.Add(err)
			handler.SendResponse(c, err1, nil)

			c.Abort()
			return nil, nil
		}
		keyID = paramsDebug.KeyID
	} else {
		if err := c.ShouldBind(&params); err != nil {
			err1 := errcode.New(errcode.ValidationError, err)
			err1.Add(err)
			handler.SendResponse(c, err1, nil)

			c.Abort()
			return nil, nil
		}
		keyID = params.KeyID
	}

	userInfo, ok := UserInfos[keyID]
	if !ok {
		return nil, errors.New(fmt.Sprintf("KeyID '%s' not found", keyID))
	}

	keySecret, err := getKeySecret(keyID, signType, userInfo)
	if err != nil {
		return nil, err
	}

	curTimestamp := time.Now().Unix()
	if debug == "1" {
		if config.Conf().Runmode == "debug" {
			rand.Seed(curTimestamp)
			nonce := strconv.Itoa(rand.Intn(100000))
			timestamp := strconv.FormatInt(curTimestamp, 10)
			allParamsMap.Set("Timestamp", timestamp)
			allParamsMap.Set("Nonce", nonce)
			strForSign := createStrForSign(c, allParamsMap)

			signature, err := generateSign(strForSign, keySecret, signType)
			if err != nil {
				return nil, err
			}

			res := map[string]string{
				"Timestamp": timestamp,
				"Nonce":     nonce,
				"Signature": signature,
			}
			return res, nil
		}

		return nil, errors.New("debug forbidden in release runmode")
	}

	apisignLifetime := config.Conf().Auth.APISignLifetime

	if params.Timestamp.Unix() > curTimestamp ||
		curTimestamp-params.Timestamp.Unix() >= apisignLifetime {
		return nil, errors.New("Signature expired")
	}

	strForSign := createStrForSign(c, allParamsMap)

	err = verifySiganature(strForSign, signType, keySecret, params.Signature, userInfo)

	return nil, err
}

func verifySiganature(strForSign, signType, keySecret, requestSignature string,
	userInfo userInfo) error {
	switch signType {
	case "md5":
		signature, err := generateSign(strForSign, keySecret, signType)
		if err != nil {
			return err
		}
		if requestSignature != signature {
			return errors.New("Signature invalid")
		}
	case "aes":
		srcStr, err := AESDecrypt(requestSignature, keySecret)
		if err != nil {
			return err
		}

		if srcStr != strForSign {
			return errors.New("Signature invalid")
		}
	case "rsa":
		srcStr, err := DecryptByPrivate(requestSignature, userInfo.RSA.Private)
		if err != nil {
			return err
		}

		if srcStr != strForSign {
			return errors.New("Signature invalid")
		}
	case "hmac_md5", "hmac_sha1", "hmac_sha256":
		signature, err := generateSign(strForSign, keySecret, signType)
		if err != nil {
			return err
		}

		if requestSignature != signature {
			return errors.New("Signature invalid")
		}
	default:
		return errors.New("Signature encrypt type invalid")
	}

	return nil
}

func getKeySecret(keyID, signType string, userInfo userInfo) (string, error) {
	keySecret := ""
	switch signType {
	case "md5":
		keySecret = userInfo.MD5
	case "aes":
		keySecret = userInfo.AES
	case "rsa":
		keySecret = userInfo.RSA.Public
	case "hmac_md5", "hmac_sha1", "hmac_sha256":
		keySecret = userInfo.Hmac
	}

	return keySecret, nil
}

func createStrForSign(c *gin.Context, reqParamsMap url.Values) string {
	var keys []string
	for k := range reqParamsMap {
		if k != "Signature" && k != "debug" {
			keys = append(keys, k)
		}
	}

	sort.Strings(keys)

	params := ""
	for i := 0; i < len(keys); i++ {
		params = params + fmt.Sprintf("%v=%v", keys[i], reqParamsMap.Get(keys[i]))
	}

	reqMethod := c.Request.Method
	reqHost := strings.Split(c.Request.Host, ":")[0]
	reqPath := c.FullPath()

	return reqMethod + reqHost + reqPath + params
}

func generateSign(strForSign, keySecret, signType string) (string, error) {
	signature := ""
	var err error
	switch signType {
	case "md5":
		signature = Md5sum(keySecret + strForSign + keySecret)
	case "aes":
		signature, err = AESEncrypt(strForSign, keySecret)
		if err != nil {
			return "", err
		}
	case "rsa":
		signature, err = EncryptByPublic(strForSign, keySecret)
		if err != nil {
			return "", err
		}
	case "hmac_md5", "hmac_sha1", "hmac_sha256":
		signature, err = Hmac(signType, strForSign, keySecret)
		if err != nil {
			return "", err
		}

	}

	return signature, nil
}
