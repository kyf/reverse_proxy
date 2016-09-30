package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"github.com/Unknwon/goconfig"
	"github.com/gorilla/websocket"
	mlog "github.com/kyf/util/log"
)

const (
	ADDR       = ":443"
	LOG_DIR    = "/var/log/myproxy/"
	LOG_PREFIX = "[myproxy]"
)

var (
	ROOT_DIR    = os.Getenv("ROOT_DIR")
	CERT_FILE   = ROOT_DIR + "/cert/molu.crt"
	KEY_FILE    = ROOT_DIR + "/cert/molu.key"
	CONFIG_PATH = ROOT_DIR + "/conf/proxy.ini"
)

type Proxy struct {
	logger    *log.Logger
	hm        map[string]string
	httpProxy *httputil.ReverseProxy
	wsProxy   *WebsocketReverseProxy
}

func readMessage(conn *websocket.Conn, msg chan<- string, mt, exit chan<- int) {
	for {
		_mt, _msg, err := conn.ReadMessage()
		if err != nil {
			log.Printf("read message error:%v", err)
			exit <- 1
			return
		}
		msg <- string(_msg)
		mt <- _mt
	}
}

type WebsocketReverseProxy struct {
	logger   *log.Logger
	Director func(*http.Request)
}

func (this *WebsocketReverseProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	client := websocket.Upgrader{}
	connReq, err := client.Upgrade(w, r, nil)
	if err != nil {
		this.logger.Print(err)
		return
	}
	defer connReq.Close()

	this.Director(r)
	if r.Host == "" {
		this.logger.Print("host invalid")
		w.Write([]byte("host invalid"))
		return
	}
	u := url.URL{Scheme: r.URL.Scheme, Host: r.Host, Path: r.URL.Path}

	connRes, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		this.logger.Print("dial:", err)
		return
	}
	defer connRes.Close()

	exitReq := make(chan int, 1)
	exitRes := make(chan int, 1)

	mtReq := make(chan int, 1)
	mtRes := make(chan int, 1)

	msgReq := make(chan string, 1)
	msgRes := make(chan string, 1)

	go readMessage(connReq, msgReq, mtReq, exitReq)
	go readMessage(connRes, msgRes, mtRes, exitRes)

	for {
		select {
		case <-exitRes:
			goto Exit
		case <-exitReq:
			goto Exit
		case mReq := <-msgReq:
			if err := connRes.WriteMessage(<-mtReq, []byte(mReq)); err != nil {
				this.logger.Printf("write message error:%v", err)
				goto Exit
			}
		case mRes := <-msgRes:
			if err := connReq.WriteMessage(<-mtRes, []byte(mRes)); err != nil {
				this.logger.Printf("write message error:%v", err)
				goto Exit
			}
		}
	}

Exit:
	log.Print(r.RequestURI, " closed>>>>>>>>>>")

}

func (this *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if websocket.IsWebSocketUpgrade(r) {
		this.wsProxy.ServeHTTP(w, r)
	} else {
		this.httpProxy.ServeHTTP(w, r)
	}
}

func (this *Proxy) reset() {
	conf, err := goconfig.LoadConfigFile(CONFIG_PATH)
	if err != nil {
		this.logger.Print(err)
		return
	}

	hm := make(map[string]string)
	for _, node := range conf.GetSectionList() {
		hm[node] = conf.MustValue(node, "host")
	}

	this.hm = hm

	director := func(r *http.Request) {
		r.URL.Scheme = r.URL.Scheme
		r.URL.Host = hm[r.Host]
		r.URL.RawQuery = r.RequestURI
	}

	this.httpProxy = &httputil.ReverseProxy{Director: director, ErrorLog: this.logger}
	this.wsProxy = &WebsocketReverseProxy{Director: director, logger: this.logger}
}

func main() {
	writer, err := mlog.NewWriter(LOG_DIR)
	if err != nil {
		log.Fatal(err)
	}
	logger := log.New(writer, LOG_PREFIX, log.LstdFlags)
	pro := &Proxy{logger: logger}

	sig := make(chan os.Signal, 1)

	signal.Notify(sig, syscall.SIGUSR1)

	go func() {
		for {
			select {
			case <-sig:
				pro.reset()
			}
		}
	}()

	pro.reset()
	log.Fatal(http.ListenAndServeTLS(ADDR, CERT_FILE, KEY_FILE, pro))
}
