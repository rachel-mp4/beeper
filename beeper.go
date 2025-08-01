package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/bluesky-social/jetstream/pkg/client"
	"github.com/bluesky-social/jetstream/pkg/client/schedulers/sequential"
	"github.com/bluesky-social/jetstream/pkg/models"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gopxl/beep"
	"github.com/gopxl/beep/speaker"
	"github.com/gopxl/beep/wav"
	"log/slog"
	"os"
	"strconv"
	"time"
)

type NotificationSystem struct {
	np *NotificationPlayer
	tp *tea.Program
}

type NotificationPlayer struct {
	audioData  beep.Buffer
	sampleRate beep.SampleRate
}

func main() {
	fmt.Println("beep")
	f, err := os.Open("thread_notification.wav")
	if err != nil {
		panic(err)
	}
	streamer, format, err := wav.Decode(f)
	if err != nil {
		panic(err)
	}
	defer streamer.Close()
	buffer := beep.NewBuffer(format)
	buffer.Append(streamer)
	np := &NotificationPlayer{
		audioData:  *buffer,
		sampleRate: format.SampleRate,
	}
	err = speaker.Init(np.sampleRate, np.sampleRate.N(time.Second/10))
	if err != nil {
		panic(err)
	}
	tp := tea.NewProgram(initialModel())
	ns := &NotificationSystem{
		np,
		tp,
	}
	fmt.Println("starting!!")
	go consumeLoop(context.Background(), ns)
	go ticker(ns)
	ns.tp.Run()
}

func ticker(ns *NotificationSystem) {
	ticker := time.NewTicker(50 * time.Millisecond)
	for {
		select {

		case <-ticker.C:
			ns.tp.Send(Tick{})
		}
	}
}

func consumeLoop(ctx context.Context, ns *NotificationSystem) {
	jsServerAddr := os.Getenv("JS_SERVER_ADDR")
	if jsServerAddr == "" {
		jsServerAddr = "wss://jetstream.atproto.tools/subscribe"
	}
	consumer := NewConsumer(jsServerAddr, ns)
	for {
		err := consumer.Consume(ctx)
		if err != nil {
			fmt.Printf("error in consumeLoop: %s\n", err.Error())
			if errors.Is(err, context.Canceled) {
				fmt.Println("exiting consume loop")
				return
			}
		}
	}
}

type Consumer struct {
	cfg     *client.ClientConfig
	handler *handler
}

type handler struct {
	ns *NotificationSystem
}

func NewConsumer(jsAddr string, ns *NotificationSystem) *Consumer {
	cfg := client.DefaultClientConfig()
	if jsAddr != "" {
		cfg.WebsocketURL = jsAddr
	}
	cfg.WantedCollections = []string{
		"org.xcvr.actor.profile",
		"org.xcvr.feed.channel",
		"org.xcvr.lrc.message",
		"org.xcvr.lrc.signet",
	}
	cfg.WantedDids = []string{}
	return &Consumer{
		cfg:     cfg,
		handler: &handler{ns},
	}
}

func (c *Consumer) Consume(ctx context.Context) error {
	scheduler := sequential.NewScheduler("jetstream_localdev", slog.Default(), c.handler.HandleEvent)
	defer scheduler.Shutdown()
	client, err := client.NewClient(c.cfg, slog.Default(), scheduler)
	if err != nil {
		return errors.New("failed to create client: " + err.Error())
	}
	cursor := time.Now().Add(1 * -time.Minute).UnixMicro()
	err = client.ConnectAndRead(ctx, &cursor)
	if err != nil {
		return errors.New("error connecting and reading: " + err.Error())
	}
	return nil
}

func (h *handler) HandleEvent(ctx context.Context, event *models.Event) error {
	if event.Commit != nil {
		h.ns.Notify(&event.Commit.Collection)
	}
	return nil
}

func (ns *NotificationSystem) Notify(notification *string) {

	streamer := ns.np.audioData.Streamer(0, ns.np.audioData.Len())
	speaker.Play(streamer)
	ns.tp.Send(NotificationMsg{notification})
}

type model struct {
	records []*record
}

type record struct {
	then   *time.Time
	handle *string
}

func initialModel() model {
	return model{
		records: make([]*record, 0),
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

//6c67ea
//15191e

type NotificationMsg struct {
	handle *string
}

type Tick struct {
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "q" {

			return m, tea.Quit
		}
	case NotificationMsg:
		n := time.Now()
		record := record{
			then:   &n,
			handle: msg.handle,
		}
		m.records = append(m.records, &record)
	case Tick:
		maxidx := 0
		n := time.Now()
		for i, record := range m.records {
			if n.Sub(*record.then) > 10*time.Second {
				maxidx = i
			}
		}
		m.records = m.records[maxidx:]
	}
	return m, nil
}

func (m model) View() string {
	s := ""
	now := time.Now()
	for _, record := range m.records {
		str := "invalid handle"
		if record.handle != nil {
			str = fmt.Sprintf("%s", *record.handle)
		}
		amt := dtf(now.Sub(*record.then))
		c, err := interpolateColors("6c67ea", "DCDCDC", amt)
		if err != nil {
			c = "DCDCDC"
		}
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(c))
		s = s + style.Render(str) + "\n"
	}
	return s
}

func dtf(d time.Duration) float64 {
	if d < 0*time.Second {
		d = 0 * time.Second
	}
	if d > 1*time.Second {
		d = 1 * time.Second
	}
	return float64(d) / float64(1*time.Second)
}

func parseHexColor(hex string) (r, g, b int, err error) {
	// Remove # if present
	if len(hex) > 0 && hex[0] == '#' {
		hex = hex[1:]
	}

	if len(hex) != 6 {
		return 0, 0, 0, fmt.Errorf("invalid hex color format")
	}

	// Parse each component
	rVal, err := strconv.ParseInt(hex[0:2], 16, 0)
	if err != nil {
		return 0, 0, 0, err
	}

	gVal, err := strconv.ParseInt(hex[2:4], 16, 0)
	if err != nil {
		return 0, 0, 0, err
	}

	bVal, err := strconv.ParseInt(hex[4:6], 16, 0)
	if err != nil {
		return 0, 0, 0, err
	}

	return int(rVal), int(gVal), int(bVal), nil
}

// rgbToHex converts RGB values back to hex string
func rgbToHex(r, g, b int) string {
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

// interpolateColors linearly interpolates between two hex colors
// t should be between 0.0 and 1.0, where 0.0 returns color1 and 1.0 returns color2
func interpolateColors(color1, color2 string, t float64) (string, error) {
	// Clamp t to [0, 1]
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}

	// Parse both colors
	r1, g1, b1, err := parseHexColor(color1)
	if err != nil {
		return "", fmt.Errorf("error parsing color1: %v", err)
	}

	r2, g2, b2, err := parseHexColor(color2)
	if err != nil {
		return "", fmt.Errorf("error parsing color2: %v", err)
	}

	// Interpolate each component
	rResult := int(float64(r1)*(1-t) + float64(r2)*t)
	gResult := int(float64(g1)*(1-t) + float64(g2)*t)
	bResult := int(float64(b1)*(1-t) + float64(b2)*t)

	return rgbToHex(rResult, gResult, bResult), nil
}
