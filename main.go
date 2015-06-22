package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"golang.org/x/net/proxy"
)

var (
	flagListen = flag.String("l", ":8080", "")
	flagPorxy  = flag.String("p", "socks5://127.0.0.1:1080", "")
)

func usage() {
	fmt.Fprintf(os.Stderr, "usage: proxy [-l host] [-p host]\n")
	fmt.Fprintf(os.Stderr, "options:\n")
	flag.PrintDefaults()
	os.Exit(2)
}

func serveHTTPTunnel(w http.ResponseWriter, r *http.Request) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacker failed", http.StatusInternalServerError)
		return
	}

	conn, _, err := hj.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	proxyURL, _ := url.Parse(*flagPorxy)
	dialer, err := proxy.FromURL(proxyURL, proxy.Direct)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	dest, err := dialer.Dial("tcp", r.Host)
	if err != nil {
		fmt.Fprintf(conn,
			"HTTP/1.0 500 NewRemoteSocks failed, err:%s\r\n\r\n", err)
		return
	}
	defer dest.Close()

	if r.Body != nil {
		if _, err = io.Copy(dest, r.Body); err != nil {
			fmt.Fprintf(conn, "%d %s", http.StatusBadGateway, err.Error())
			return
		}
	}
	fmt.Fprintf(conn, "HTTP/1.0 200 Connection established\r\n\r\n")

	go io.Copy(dest, conn)
	io.Copy(conn, dest)
}

func serveHTTPProxy(w http.ResponseWriter, r *http.Request) {
	proxyURL, _ := url.Parse(*flagPorxy)
	dialer, err := proxy.FromURL(proxyURL, proxy.Direct)
	if err != nil {
		log.Println(err.Error())
		return
	}

	tr := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		Dial:                dialer.Dial,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	client := &http.Client{Transport: tr}
	r.RequestURI = ""
	resp, err := client.Do(r)
	if err != nil {
		log.Println(err.Error())
		return
	}
	defer resp.Body.Close()

	for k, v := range resp.Header {
		for _, vv := range v {
			w.Header().Add(k, vv)
		}
	}

	for _, c := range resp.Cookies() {
		w.Header().Add("Set-Cookie", c.Raw)
	}

	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func handler(w http.ResponseWriter, r *http.Request) {
	log.Println(r.Method, r.URL.Path, r.URL.Host)

	if r.Method == "CONNECT" {
		serveHTTPTunnel(w, r)
		return
	}

	serveHTTPProxy(w, r)
}

func main() {
	flag.Usage = usage
	flag.Parse()

	log.Println("Start serving on ", *flagListen)
	http.ListenAndServe(*flagListen, http.HandlerFunc(handler))
}
