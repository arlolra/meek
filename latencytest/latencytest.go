package latencytest

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"time"

	"appengine"
	"appengine/urlfetch"
)

var urls = [...]string{
	"http://meek.bamsoftware.com:7002/",
	"https://meek.bamsoftware.com:7443/",
	"http://83.212.83.23:7002/",
	"http://www.googleusercontent.com/",
	"https://www.googleusercontent.com/",
}

const urlFetchTimeout = 10 * time.Second
const numTrials = 5

var context appengine.Context

type record struct {
	Duration float64
	Status   string
	Err      error
}

func timeGet(u string) (rec *record) {
	rec = new(record)

	transport := urlfetch.Transport{
		Context:  context,
		Deadline: urlFetchTimeout,
	}
	req, err := http.NewRequest("POST", u, nil)
	if err != nil {
		rec.Err = err
		return rec
	}
	start := time.Now()
	resp, err := transport.RoundTrip(req)
	end := time.Now()
	if err != nil {
		rec.Err = err
		return rec
	}
	defer resp.Body.Close()

	rec.Duration = end.Sub(start).Seconds()
	rec.Status = resp.Status

	return rec
}

func handler(w http.ResponseWriter, r *http.Request) {
	context = appengine.NewContext(r)

	records := make(map[string][]*record)
	for _, u := range urls {
		records[u] = make([]*record, 0, numTrials)
	}
	for i := 0; i < numTrials; i++ {
		for _, u := range urls {
			records[u] = append(records[u], timeGet(u))
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	fmt.Fprint(w, `<!DOCTYPE html>
<html>
<head>
<meta charset=utf-8>
<title>Latency test</title>
</head>
<body>
<!-- http://dimplejs.org/examples_viewer.html?id=scatter_vertical_lollipop -->
<div id="chartContainer">
  <script src="http://d3js.org/d3.v3.min.js"></script>
  <script src="http://dimplejs.org/dist/dimple.v1.1.5.min.js"></script>
  <script type="text/javascript">
    var svg = dimple.newSvg("#chartContainer", 800, 400);
    var data = [
`)
	for _, u := range urls {
		for _, rec := range records[u] {
			if rec.Err != nil {
				continue
			}
			row := struct {
				URL     string
				Latency float64
			}{
				URL:     u,
				Latency: rec.Duration * 1000,
			}
			rep, err := json.Marshal(row)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "      %s,\n", rep)
		}
	}

	fmt.Fprint(w, `    ];
    var myChart = new dimple.chart(svg, data);
    myChart.setBounds(230, 20, "100%,-250", "100%,-70")
    myChart.addMeasureAxis("x", "Latency");
    var y = myChart.addCategoryAxis("y", "URL");
`)
	rep, err := json.Marshal(urls)
	if err == nil {
		fmt.Fprintf(w, "    var urls = %s;\n", rep)
		fmt.Fprintf(w, "    urls.reverse();\n")
		fmt.Fprintf(w, "    y.addOrderRule(urls);\n")
	}
	fmt.Fprint(w, `    myChart.addSeries(["Latency", "URL"], dimple.plot.bubble);
    myChart.draw();
  </script>
</div>
`)
	fmt.Fprint(w, "<table>\n")
	for _, u := range urls {
		var sum float64
		fmt.Fprintf(w, "<tr><th colspan=3 align=center bgcolor=lightblue>%s</th></tr>\n", html.EscapeString(u))
		for i, rec := range records[u] {
			var status string
			if rec.Err != nil {
				status = rec.Err.Error()
			} else {
				status = rec.Status
				sum += rec.Duration
			}
			fmt.Fprintf(w, "<tr><td align=right>%d</td><td align=right>%s</td><td align=right>%.2f&nbsp;ms</td></tr>\n",
				i, html.EscapeString(status), rec.Duration*1000)
		}
		fmt.Fprintf(w, "<tr><td align=right><b>avg</b></td><td></td><td align=right><b>%.2f&nbsp;ms</b></td></tr>\n",
			sum/float64(len(records[u]))*1000)
	}
	fmt.Fprint(w, "</table>\n")

}

func init() {
	http.HandleFunc("/", handler)
}
