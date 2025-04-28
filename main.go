package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"github.com/tidwall/gjson"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/scrapeless-ai/scrapeless-actor-sdk-go/scrapeless"
	proxyModel "github.com/scrapeless-ai/scrapeless-actor-sdk-go/scrapeless/proxy"
	log "github.com/sirupsen/logrus"
)

var (
	client *http.Client
)

type RequestParam struct {
	Trend string `json:"trend" url:"trend"`
	Hl    string `json:"hl" url:"hl"`
	Gl    string `json:"gl" url:"gl"`
}

func main() {
	// new actor
	actor := scrapeless.New(scrapeless.WithProxy(), scrapeless.WithStorage())
	defer actor.Close()
	var param = &RequestParam{}
	if err := actor.Input(param); err != nil {
		log.Fatal(err)
	}

	proxy, err := actor.Proxy.Proxy(context.TODO(), proxyModel.ProxyActor{
		Country:         "us",
		SessionDuration: 10,
	})

	if err != nil {
		panic(err)
	}
	parse, err := url.Parse(proxy)
	if err != nil {
		panic(err)
	}
	// init client with proxy
	client = &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(parse)}}

	data, err := GetFinanceMarket(context.TODO(), param.Trend, param.Hl, param.Gl)
	if err != nil {
		log.Fatal(err)
	}
	resultBytes, _ := json.Marshal(data)
	log.Println(string(resultBytes))

	ok, err := actor.Storage.GetDataset().AddItems(context.Background(), []map[string]any{
		{
			"data": string(resultBytes),
		},
	})

	if !ok || err != nil {
		log.Errorf("set kv failed,  err=%v", err)
		return
	}
	log.Info("set kv success")
}

func GetFinanceMarket(ctx context.Context, trend string, hl string, gl string) (any, error) {
	var (
		marketsInfo MarketsInfo
	)
	urlStr := fmt.Sprintf("https://www.google.com/finance/markets/%s?hl=%s", trend, hl)
	if gl != "" {
		urlStr = fmt.Sprintf("https://www.google.com/finance/markets/%s?hl=%s&gl=%s", trend, hl, gl)
	}
	res, err := getData(ctx, urlStr)
	if err != nil {
		return nil, err
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(res))
	if err != nil {
		return nil, err
	}
	mapping := GetDataMapping(res)
	trends := MarketTrends(doc)
	newsResults := NewsResultsMarkets(doc)
	discover := DiscoverMarkets(doc)
	markets := Markets(mapping["5"], GetMarketTitle(doc))
	marketsInfo.Markets = markets
	marketsInfo.MarketTrends = trends
	marketsInfo.DiscoverMore = discover
	marketsInfo.NewsResults = newsResults
	return marketsInfo, nil
}

type MarketsInfo struct {
	Markets      any `json:"markets"`
	MarketTrends any `json:"market_trends,omitempty"`
	NewsResults  any `json:"news_results"`
	DiscoverMore any `json:"discover_more"`
}

type MarketTrendsInfo struct {
	Title    string `json:"title"`
	Link     string `json:"link,omitempty"`
	Subtitle string `json:"subtitle,omitempty"`
	Results  any    `json:"results"`
}

type MarketTrendsResults struct {
	Stock          string  `json:"stock"`
	Link           string  `json:"link"`
	Name           string  `json:"name"`
	Price          string  `json:"price"`
	ExtractedPrice float64 `json:"extracted_price"`
	Currency       string  `json:"currency,omitempty"`
	PriceMovement  PriceMovement
}
type PriceMovement struct {
	Value      float64 `json:"value,omitempty"`
	Percentage float64 `json:"percentage"`
	Movement   string  `json:"movement"`
}

func getData(ctx context.Context, path string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, "GET", path, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/114.0.0.0 Safari/537.36")
	request.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/jpeg,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
	request.Header.Set("Accept-Language", "en-US,en;q=0.9")
	request.Header.Set("Cache-Control", "no-cache")
	request.Header.Set("Pragma", "no-cache")
	request.Header.Set("Priority", "u=0, i")
	request.Header.Set("Sec-Ch-Ua", `"Not A(Brand";v="8", "Chromium";v="132", "Google Chrome";v="132"`)
	request.Header.Set("Sec-Ch-Ua-Mobile", "?0")
	request.Header.Set("Sec-Ch-Ua-Platform", `"Windows"`)
	request.Header.Set("Sec-Fetch-Dest", "document")
	request.Header.Set("Sec-Fetch-Mode", "navigate")
	request.Header.Set("Sec-Fetch-Site", "none")
	request.Header.Set("Sec-Fetch-User", "?1")
	request.Header.Set("Upgrade-Insecure-Requests", "1")
	do, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer do.Body.Close()
	body, err := io.ReadAll(do.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func GetDataMapping(data string) map[string]string {
	var (
		dataMapping = make(map[string]string)
	)
	re := regexp.MustCompile(`key:.*?sideChannel?`)
	re1 := regexp.MustCompile(`key: '([^']+)'`)
	re2 := regexp.MustCompile(`data:.*?sideChannel?`)
	matchString := re.FindAllString(data, -1)
	for _, v := range matchString {
		key := re1.FindAllString(v, -1)
		a := strings.Replace(key[0], `'`, "", -1)
		split := strings.Split(a, ":")
		dataRes := re2.FindAllString(v, -1)
		s := strings.Replace(dataRes[0], "data:", "", -1)
		s = strings.Replace(s, ", sideChannel", "", -1)
		dataMapping[split[len(split)-1]] = s
	}
	return dataMapping
}

func MarketTrends(dc *goquery.Document) any {
	var (
		marketTrendsInfos []MarketTrendsInfo
	)
	dc.Find("div[class='Vd323d']").Children().Each(func(i int, s *goquery.Selection) {
		marketTrendsInfo := MarketTrendsInfo{}
		marketTrendsResults := make([]MarketTrendsResults, 0)
		subtitle := s.Children().Eq(0).Children().Eq(0).Find("span[class='MzhJl']").Text()
		marketTrendsInfo.Subtitle = subtitle
		linkArray, _ := s.Children().Eq(0).Children().Eq(0).Children().Eq(0).Attr("href")
		if len(linkArray) != 0 {
			marketTrendsInfo.Link = fmt.Sprintf("https://www.google.com/finance%s", linkArray[1:])
		}
		s.Children().Eq(0).Children().Eq(0).Contents().Each(func(i int, selection *goquery.Selection) {
			if goquery.NodeName(selection) == "#text" {
				marketTrendsInfo.Title = selection.Text()
			}
		})
		s.Find("li").Each(func(i int, selection *goquery.Selection) {
			val, _ := selection.Children().Eq(0).Attr("href")
			stockArray := strings.Split(val, "/")
			stock := stockArray[len(stockArray)-1]
			link := fmt.Sprintf("https://www.google.com/finance/quote/%s", stock)

			classSxcTic := selection.Children().Eq(0).Children().Eq(0).Children().Eq(0)
			name := classSxcTic.Children().Eq(0).Children().Eq(1).Text()
			price := classSxcTic.Children().Eq(1).Text()
			currency := ""
			if strings.Contains(price, "$") {
				currency = price[0:1]
				price = price[1:]
			}
			priceMovementValueStr := classSxcTic.Children().Eq(2).Text()
			priceMovementValueStr = strings.Replace(priceMovementValueStr, "$", "", -1)
			priceMovementPercentageStr := classSxcTic.Children().Eq(3).Text()
			priceMovementPercentage, _ := strconv.ParseFloat(priceMovementPercentageStr[:len(priceMovementPercentageStr)-1], 64)
			extractedPrice, _ := strconv.ParseFloat(strings.Replace(price, ",", "", -1), 64)
			priceMovementMovement := "Down"
			if priceMovementValueStr[0] == '+' {
				priceMovementMovement = "Up"
			}
			priceMovementValue, _ := strconv.ParseFloat(priceMovementValueStr[1:], 64)
			marketTrendsResults = append(marketTrendsResults, MarketTrendsResults{
				Stock:          stock,
				Link:           link,
				Name:           name,
				Price:          price,
				Currency:       currency,
				ExtractedPrice: extractedPrice,
				PriceMovement: PriceMovement{
					Value:      priceMovementValue,
					Percentage: priceMovementPercentage,
					Movement:   priceMovementMovement,
				},
			})
		})
		marketTrendsInfo.Results = marketTrendsResults
		marketTrendsInfos = append(marketTrendsInfos, marketTrendsInfo)
	})
	return marketTrendsInfos
}

type NewsResultsInfo struct {
	Snippet   string `json:"snippet"`
	Link      string `json:"link"`
	Source    string `json:"source"`
	Date      string `json:"date"`
	Thumbnail string `json:"thumbnail,omitempty"`
}

func NewsResultsMarkets(dc *goquery.Document) []NewsResultsInfo {
	var (
		resp = make([]NewsResultsInfo, 0)
	)
	dc.
		Find("div[jscontroller='ZpnVYd'] div[class='nkXTJ']").
		Each(func(i int, selection *goquery.Selection) {
			source := selection.Children().Eq(0).Children().Eq(0).Children().Eq(0).Children().Eq(0).Children().Eq(0).Text()
			date := selection.Children().Eq(0).Children().Eq(0).Children().Eq(0).Children().Eq(0).Children().Eq(1).Text()
			snippet := selection.Children().Eq(0).Children().Eq(0).Children().Eq(0).Children().Eq(1).Text()
			link, _ := selection.Children().Eq(1).Children().Eq(0).Attr("href")
			thumbnail, _ := selection.Children().Eq(1).Children().Eq(0).Children().Eq(0).Attr("src")
			resp = append(resp, NewsResultsInfo{
				Snippet:   snippet,
				Link:      link,
				Source:    source,
				Date:      date,
				Thumbnail: thumbnail,
			})
		})
	return resp
}

type DiscoverAndPeopleSearchInfo struct {
	Title string                        `json:"title"`
	Items []DiscoverAndPeopleSearchItem `json:"items"`
}
type DiscoverAndPeopleSearchItem struct {
	Stock          string  `json:"stock"`
	Link           string  `json:"link"`
	Name           string  `json:"name"`
	Price          string  `json:"price"`
	ExtractedPrice float64 `json:"extracted_price"`
	Currency       string  `json:"currency,omitempty"`
	PriceMovement  PriceMovement
}

func DiscoverMarkets(dc *goquery.Document) any {
	var (
		discoverAndPeopleSearchInfo []DiscoverAndPeopleSearchInfo
		interestedIn                DiscoverAndPeopleSearchInfo
	)
	// You may be interested in
	dc.Find("section[role='complementary']").Children().Eq(1).Children().Eq(0).Contents().Each(func(i int, selection *goquery.Selection) {
		if goquery.NodeName(selection) == "#text" {
			title := strings.TrimSpace(selection.Text())
			interestedIn.Title = title
		}
	})

	dc.Find("section[role='complementary']").Children().Eq(1).Children().Eq(1).Children().Eq(0).Children().Eq(0).Find("div[role='listitem']").Each(func(i int, selection *goquery.Selection) {
		a := selection.Children().Eq(0).Children().Eq(0).Children().Eq(0)
		stock, _ := a.Attr("href")
		split := strings.Split(stock, `/`)
		stock = split[len(split)-1]
		link := fmt.Sprintf("https://www.google.com/finance/quote/%s", stock)
		name := a.Contents().Eq(1).Text()
		percentage := a.Children().Eq(2).Find("span[jsname='Fe7oBc']").Children().Eq(0).Children().Eq(0).Contents().Eq(1).Text()
		movement, _ := a.Children().Eq(2).Find("span[jsname='Fe7oBc']").Children().Eq(0).Children().Eq(0).Find("path").Attr("d")
		if strings.Contains(movement, "M20") {
			movement = "Down"
		} else {
			movement = "Up"
		}
		price := a.Children().Eq(2).Children().Eq(0).Text()
		currency := ""
		if !isFirstDigit(price) {
			currency = price[:1]
			price = price[1:]
		}
		replacePrice := strings.Replace(price, ",", "", -1)
		priceF, _ := strconv.ParseFloat(replacePrice, 64)
		percentageF, _ := strconv.ParseFloat(strings.Replace(percentage, "%", "", -1), 64)
		interestedIn.Items = append(interestedIn.Items, DiscoverAndPeopleSearchItem{
			Stock:          stock,
			Link:           link,
			Name:           name,
			Price:          price,
			ExtractedPrice: priceF,
			Currency:       currency,
			PriceMovement: PriceMovement{
				Movement:   movement,
				Percentage: percentageF,
			},
		})
	})
	discoverAndPeopleSearchInfo = append(discoverAndPeopleSearchInfo, interestedIn)
	return discoverAndPeopleSearchInfo
}

func isFirstDigit(s string) bool {
	if len(s) == 0 {
		return false
	}
	return unicode.IsDigit(rune(s[0]))
}

func Markets(data string, marketTitle []string) map[string]any {
	var (
		resp  = make(map[string]any)
		count = 0
		index = 0
	)
	for _, result := range gjson.Parse(data).Get("0").Array() {
		for _, k := range result.Array() {
			for _, k1 := range k.Array() {
				if k1.IsArray() {
					if count%5 == 0 && count != 0 {
						index++
					}
					info := parseMarkets(k1.String())
					if resp[marketTitle[index]] == nil {
						resp[marketTitle[index]] = make([]any, 0)
					}
					resp[marketTitle[index]] = append(resp[marketTitle[index]].([]any), info)
					count++
				}
			}
		}
	}
	return resp
}
func parseMarkets(data string) (marketsInfo MarketsInfoInner) {
	d := gjson.Parse(data)
	name := d.Get("2").String()
	if name == "" {
		name = d.Get("1.0.2").String()
	}
	info := d.Get("1.0")
	stock := info.Get("21").String()
	link := fmt.Sprintf("https://www.google.com/finance/quote/%s", stock)
	price := info.Get("5.0").Float()                   // 内部的参数
	priceMovementValue := info.Get("5.1").Float()      // 内部的参数
	priceMovementPercentage := info.Get("5.2").Float() // 内部的参数
	priceMovementMovement := "Up"
	if priceMovementPercentage < 0 {
		priceMovementMovement = "Down"
	}
	return MarketsInfoInner{
		Stock: stock,
		Link:  link,
		Name:  name,
		Price: price,
		PriceMovement: PriceMovement{
			Value:      priceMovementValue,
			Percentage: priceMovementPercentage,
			Movement:   priceMovementMovement,
		},
	}
}

type MarketsInfoInner struct {
	Stock         string        `json:"stock"`
	Link          string        `json:"link"`
	Name          string        `json:"name"`
	Price         float64       `json:"price"`
	PriceMovement PriceMovement `json:"price_movement"`
}

func GetMarketTitle(dc *goquery.Document) (marketTitle []string) {
	dc.Find("div[jscontroller='LMhoGc']").Children().Eq(1).Children().Eq(0).Children().Children().Each(func(i int, selection *goquery.Selection) {
		val, _ := selection.Attr("class")
		if strings.Contains(val, "AHyjFe") {
			marketTitle = append(marketTitle, selection.Text())
		}
	})
	return
}
