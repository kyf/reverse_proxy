package main

import (
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
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

func readMessage(writer io.Writer, reader io.Reader, errChan chan<- error) {
	_, err := io.Copy(writer, reader)
	if err != nil {
		errChan <- err
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
	u := r.URL

	header := make(http.Header)
	header.Set("Cookie", r.Header.Get("Cookie"))
	connRes, wsres, err := websocket.DefaultDialer.Dial(u.String(), header)
	if err != nil {
		body, _ := ioutil.ReadAll(wsres.Body)
		this.logger.Print("dial:", u.String(), err, string(body))
		return
	}
	defer connRes.Close()

	exit := make(chan error, 1)

	go readMessage(connReq.UnderlyingConn(), connRes.UnderlyingConn(), exit)
	go readMessage(connRes.UnderlyingConn(), connReq.UnderlyingConn(), exit)

	err = <-exit
	this.logger.Print("closed>>>>>>>>>>", err)

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
		if websocket.IsWebSocketUpgrade(r) {
			r.URL.Scheme = "ws"
		} else {
			r.URL.Scheme = "http"
		}
		r.URL.Host = hm[r.Host]
		//r.URL.RawQuery = r.RequestURI
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
	//log.Fatal(http.ListenAndServe(":1280", pro))
}
