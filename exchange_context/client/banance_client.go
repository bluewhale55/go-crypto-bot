package client

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	uuid2 "github.com/google/uuid"
	"github.com/gorilla/websocket"
	"gitlab.com/open-soft/go-crypto-bot/exchange_context/model"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Binance struct {
	ApiKey         string
	ApiSecret      string
	DestinationURI string

	HttpClient   *http.Client
	connection   *websocket.Conn
	channel      chan []byte
	socketWriter chan []byte
}

func (b *Binance) Connect() {
	connection, _, err := websocket.DefaultDialer.Dial("wss://testnet.binance.vision/ws-api/v3", nil)
	if err != nil {
		log.Fatal("dial:", err)
	}
	b.channel = make(chan []byte)
	b.socketWriter = make(chan []byte)

	// reader channel
	go func() {
		for {
			_, message, err := connection.ReadMessage()
			if err != nil {
				log.Println("read: ", err)

				os.Exit(1)
			}

			b.channel <- message
		}
	}()

	// writer channel
	go func() {
		for {
			serialized := <-b.socketWriter
			_ = b.connection.WriteMessage(websocket.TextMessage, serialized)
		}
	}()

	b.connection = connection
}

func (b *Binance) socketRequest(req model.SocketRequest, channel chan []byte) {
	go func() {
		for {
			msg := <-b.channel

			if strings.Contains(string(msg), req.Id) {
				channel <- msg
				return
			}

			b.channel <- msg
		}
	}()

	serialized, _ := json.Marshal(req)
	b.socketWriter <- serialized
}

func (b *Binance) QueryOrder(symbol string, orderId int64) (model.BinanceOrder, error) {
	channel := make(chan []byte)
	defer close(channel)

	socketRequest := model.SocketRequest{
		Id:     uuid2.New().String(),
		Method: "order.status",
		Params: make(map[string]any),
	}
	socketRequest.Params["apiKey"] = b.ApiKey
	socketRequest.Params["orderId"] = orderId
	socketRequest.Params["symbol"] = symbol
	socketRequest.Params["timestamp"] = time.Now().Unix() * 1000
	socketRequest.Params["signature"] = b.signature(socketRequest.Params)
	b.socketRequest(socketRequest, channel)
	message := <-channel

	var response model.BinanceOrderResponse
	json.Unmarshal(message, &response)

	if response.Error != nil {
		return model.BinanceOrder{}, errors.New(response.Error.Message)
	}

	return response.Result, nil
}

func (b *Binance) CancelOrder(symbol string, orderId int64) (model.BinanceOrder, error) {
	channel := make(chan []byte)
	defer close(channel)

	socketRequest := model.SocketRequest{
		Id:     uuid2.New().String(),
		Method: "order.cancel",
		Params: make(map[string]any),
	}
	socketRequest.Params["apiKey"] = b.ApiKey
	socketRequest.Params["orderId"] = orderId
	socketRequest.Params["symbol"] = symbol
	socketRequest.Params["timestamp"] = time.Now().Unix() * 1000
	socketRequest.Params["signature"] = b.signature(socketRequest.Params)
	b.socketRequest(socketRequest, channel)
	message := <-channel

	var response model.BinanceOrderResponse
	json.Unmarshal(message, &response)

	if response.Error != nil {
		return model.BinanceOrder{}, errors.New(response.Error.Message)
	}

	return response.Result, nil
}

func (b *Binance) GetOpenedOrders() (*[]model.BinanceOrder, error) {
	queryString := fmt.Sprintf(
		"timestamp=%d",
		time.Now().UTC().Unix()*1000,
	)
	request, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v3/openOrders?%s&signature=%s", b.DestinationURI, queryString, b.sign(queryString)), nil)
	request.Header.Set("Content-Type", "application/json; charset=UTF-8")
	request.Header.Set("X-MBX-APIKEY", b.ApiKey)

	response, err := b.HttpClient.Do(request)
	if err != nil {
		return nil, err
	}

	defer response.Body.Close()
	body, err := ioutil.ReadAll(response.Body)

	if err != nil {
		return nil, err
	}

	if response.StatusCode != 200 {
		return nil, errors.New(string(body))
	}

	var binanceOrders []model.BinanceOrder
	json.Unmarshal(body, &binanceOrders)

	return &binanceOrders, nil
}

func (b *Binance) LimitOrder(order model.Order, operation string) (model.BinanceOrder, error) {
	channel := make(chan []byte)
	defer close(channel)

	socketRequest := model.SocketRequest{
		Id:     uuid2.New().String(),
		Method: "order.place",
		Params: make(map[string]any),
	}
	socketRequest.Params["symbol"] = order.Symbol
	socketRequest.Params["side"] = operation
	socketRequest.Params["type"] = "LIMIT"
	socketRequest.Params["quantity"] = strconv.FormatFloat(order.Quantity, 'f', -1, 64)
	socketRequest.Params["timeInForce"] = "GTC"
	socketRequest.Params["price"] = strconv.FormatFloat(order.Price, 'f', -1, 64)
	socketRequest.Params["apiKey"] = b.ApiKey
	socketRequest.Params["timestamp"] = time.Now().Unix() * 1000
	socketRequest.Params["signature"] = b.signature(socketRequest.Params)
	b.socketRequest(socketRequest, channel)
	message := <-channel

	var response model.BinanceOrderResponse
	json.Unmarshal(message, &response)

	if response.Error != nil {
		return model.BinanceOrder{}, errors.New(response.Error.Message)
	}

	return response.Result, nil
}

func (b *Binance) signature(params map[string]any) string {
	parts := make([]string, 0)

	keys := make([]string, 0, len(params))

	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", key, params[key]))
	}

	mac := hmac.New(sha256.New, []byte(b.ApiSecret))
	mac.Write([]byte(strings.Join(parts, "&")))
	signingKey := fmt.Sprintf("%x", mac.Sum(nil))

	return signingKey
}

func (b *Binance) sign(url string) string {
	mac := hmac.New(sha256.New, []byte(b.ApiSecret))
	mac.Write([]byte(url))
	signingKey := fmt.Sprintf("%x", mac.Sum(nil))

	return signingKey
}
