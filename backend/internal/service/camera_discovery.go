package service

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/nvr/backend/internal/domain"
)

// Пассивное обнаружение камер.
//
// Раньше сканер знал только активный перебор: пройти по всем 254 адресам,
// проверить порты и опросить каждый живой адрес. Это занимает десятки
// секунд, шлёт в сеть перебор паролей и не видит камеру, у которой закрыт
// HTTP или RTSP на стандартных портах.
//
// Камеры на OpenIPC (и большинство ONVIF-камер других марок) сами
// объявляют о себе тремя способами, и все три можно слушать, ничего не
// подбирая:
//
//   - WS-Discovery: ответ на Probe приходит с адресом ONVIF-службы;
//   - mDNS: камера отвечает на openipc.local и публикует службы _onvif/_rtsp;
//   - реестр соседей OpenIPC: любая наша камера отдаёт /api/v1/peers со
//     списком камер, которые она видит в своей подсети.
//
// Найденные так адреса дальше проходят обычный опрос (probeCamera): он
// заполняет модель, прошивку и пути потоков. Ускоряется именно поиск —
// вместо всей подсети опрашиваются только те, кто сам назвался.

// Коды источников обнаружения. Коды, а не готовые подписи: интерфейс
// переводится на несколько языков, и подпись ставит он.
const (
	SourceScan  = "scan"  // активный перебор подсети
	SourceWSD   = "wsd"   // WS-Discovery (ONVIF)
	SourceMDNS  = "mdns"  // mDNS: openipc.local и службы _onvif/_rtsp
	SourcePeers = "peers" // реестр соседей OpenIPC
)

// passiveHit — устройство, о котором рассказала сеть.
type passiveHit struct {
	IP      string
	Name    string
	Model   string
	Sources []string

	// OpenIPC — устройство назвало себя камерой OpenIPC (флаг из реестра
	// соседей). Нужен, чтобы подписать находку правильно: в списке соседей
	// бывают и чужие ONVIF-камеры, и выдавать их за OpenIPC нельзя.
	OpenIPC bool

	// ONVIFURL — адрес службы устройства из ProbeMatches. Пустой, если
	// устройство найдено только через mDNS или реестр соседей.
	ONVIFURL string
}

// addSource добавляет источник, не повторяя уже записанный.
func (h *passiveHit) addSource(src string) {
	for _, s := range h.Sources {
		if s == src {
			return
		}
	}
	h.Sources = append(h.Sources, src)
}

// passiveIndex собирает находки разных механизмов по IP.
type passiveIndex struct {
	byIP map[string]*passiveHit
}

func newPassiveIndex() *passiveIndex {
	return &passiveIndex{byIP: make(map[string]*passiveHit)}
}

func (p *passiveIndex) add(h passiveHit) {
	if h.IP == "" {
		return
	}
	cur, ok := p.byIP[h.IP]
	if !ok {
		copied := h
		p.byIP[h.IP] = &copied
		return
	}
	// Данные дополняем, а не перезаписываем: имя могло прийти из mDNS,
	// а адрес ONVIF — из WS-Discovery.
	if cur.Name == "" {
		cur.Name = h.Name
	}
	if cur.Model == "" {
		cur.Model = h.Model
	}
	if cur.ONVIFURL == "" {
		cur.ONVIFURL = h.ONVIFURL
	}
	for _, s := range h.Sources {
		cur.addSource(s)
	}
}

func (p *passiveIndex) get(ip string) *passiveHit {
	return p.byIP[ip]
}

func (p *passiveIndex) ips() []string {
	out := make([]string, 0, len(p.byIP))
	for ip := range p.byIP {
		out = append(out, ip)
	}
	return out
}

// discoverPassive запускает все пассивные механизмы одновременно.
//
// Параллельно, потому что каждый ждёт ответа по сети: последовательный
// запуск сложил бы их тайм-ауты, и скан стал бы медленнее, чем был.
// Общий срок — discoveryWindow: за это время отвечают даже камеры,
// которые ждут первого запроса (спящий mDNS-ответчик).
func (s *CameraScanner) discoverPassive(ctx context.Context) *passiveIndex {
	idx := newPassiveIndex()

	type result struct {
		hits []passiveHit
	}
	results := make(chan result, 3)

	go func() { results <- result{hits: discoverWSD(ctx, discoveryWindow)} }()
	go func() { results <- result{hits: discoverMDNS(ctx, discoveryWindow)} }()
	go func() { results <- result{hits: s.discoverPeers(ctx, discoveryWindow)} }()

	for i := 0; i < 3; i++ {
		for _, h := range (<-results).hits {
			idx.add(h)
		}
	}
	return idx
}

// discoveryWindow — сколько ждём пассивных ответов.
//
// 1,5 секунды: ответ на Probe приходит за единицы миллисекунд, mDNS — за
// десятки. Более длинное ожидание заметно удлиняло бы каждый скан, а
// находки после этого срока всё равно редки.
const discoveryWindow = 1500 * time.Millisecond

// ---------------------------------------------------------------- WS-Discovery

// wsdProbe формирует SOAP-запрос Probe по спецификации WS-Discovery 2005/04.
//
// uuid в запросе обязателен: без него часть камер не отвечает, потому что
// считает сообщение повтором уже обработанного.
func wsdProbe(messageID string) []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<e:Envelope xmlns:e="http://www.w3.org/2003/05/soap-envelope"
            xmlns:w="http://schemas.xmlsoap.org/ws/2004/08/addressing"
            xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery"
            xmlns:dn="http://www.onvif.org/ver10/network/wsdl">
  <e:Header>
    <w:MessageID>uuid:` + messageID + `</w:MessageID>
    <w:To e:mustUnderstand="true">urn:schemas-xmlsoap-org:ws:2005:04:discovery</w:To>
    <w:Action e:mustUnderstand="true">http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe</w:Action>
  </e:Header>
  <e:Body>
    <d:Probe>
      <d:Types>dn:NetworkVideoTransmitter</d:Types>
    </d:Probe>
  </e:Body>
</e:Envelope>`)
}

// wsdEnvelope — разбор ответа ProbeMatches.
//
// Теги заданы без пространства имён: encoding/xml в таком случае сверяет
// только локальное имя, а префиксы у разных прошивок разные (wsd, d, wsd10).
type wsdEnvelope struct {
	Body struct {
		ProbeMatches struct {
			ProbeMatch []struct {
				Scopes  string `xml:"Scopes"`
				XAddrs  string `xml:"XAddrs"`
				Types   string `xml:"Types"`
				EPRAddr string `xml:"EndpointReference>Address"`
			} `xml:"ProbeMatch"`
		} `xml:"ProbeMatches"`
		// Некоторые камеры присылают Hello вместо ProbeMatches — состав
		// полей тот же, поэтому разбираем оба варианта.
		Hello struct {
			Scopes  string `xml:"Scopes"`
			XAddrs  string `xml:"XAddrs"`
			EPRAddr string `xml:"EndpointReference>Address"`
		} `xml:"Hello"`
	} `xml:"Body"`
}

// parseWSD разбирает ответ камеры и возвращает находки.
//
// Вынесено отдельной функцией: разбор недоверенного XML проверяется тестом
// без сети, а на живом стенде такая проверка зависела бы от того, ответит
// ли камера в момент запуска.
func parseWSD(raw []byte) []passiveHit {
	var env wsdEnvelope
	if err := xml.Unmarshal(raw, &env); err != nil {
		return nil
	}

	var out []passiveHit

	add := func(scopes, xaddrs string) {
		hit := passiveHit{Sources: []string{SourceWSD}}
		hit.Name, hit.Model = scopeName(scopes)
		if ip := firstXAddrIP(xaddrs); ip != "" {
			hit.IP = ip
		}
		// Адрес службы оставляем только если он пригоден для запроса.
		if u := firstXAddrURL(xaddrs); u != "" {
			hit.ONVIFURL = u
		}
		if hit.IP != "" {
			out = append(out, hit)
		}
	}

	for _, m := range env.Body.ProbeMatches.ProbeMatch {
		add(m.Scopes, m.XAddrs)
	}
	if env.Body.Hello.XAddrs != "" {
		add(env.Body.Hello.Scopes, env.Body.Hello.XAddrs)
	}
	return out
}

// scopeName достаёт имя и модель из строки scopes.
//
// Формат: "onvif://www.onvif.org/name/Вход onvif://www.onvif.org/hardware/GK7205".
// Значения percent-кодированы, поэтому раскодируем их перед показом.
func scopeName(scopes string) (name, model string) {
	for _, part := range strings.Fields(scopes) {
		switch {
		case strings.Contains(part, "/name/"):
			name = decodeScopeValue(part, "/name/")
		case strings.Contains(part, "/hardware/"):
			model = decodeScopeValue(part, "/hardware/")
		}
	}
	return name, model
}

func decodeScopeValue(scope, marker string) string {
	i := strings.LastIndex(scope, marker)
	if i < 0 {
		return ""
	}
	v := scope[i+len(marker):]
	if decoded, err := url.PathUnescape(v); err == nil {
		return decoded
	}
	return v
}

// firstXAddrURL выбирает первый пригодный адрес службы.
//
// В XAddrs может быть несколько адресов через пробел (IPv4 и IPv6 одного
// устройства). Берём http(s) без учётных данных: адрес попадает в журнал
// и в интерфейс, пароли там не нужны.
func firstXAddrURL(xaddrs string) string {
	for _, raw := range strings.Fields(xaddrs) {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			continue
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			continue
		}
		if u.User != nil {
			u.User = nil
		}
		return u.String()
	}
	return ""
}

// firstXAddrIP берёт IP из первого адреса службы.
func firstXAddrIP(xaddrs string) string {
	for _, raw := range strings.Fields(xaddrs) {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			continue
		}
		host := u.Hostname()
		if net.ParseIP(host) != nil {
			return host
		}
	}
	return ""
}

// discoverWSD отправляет Probe и собирает ответы до истечения ctx.
func discoverWSD(ctx context.Context, window time.Duration) []passiveHit {
	// Слушаем на свободном порту и шлём пакет с него же: ответы приходят
	// unicast на адрес отправителя, и отдельный сокет для приёма не нужен.
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		log.Debug().Err(err).Msg("WS-Discovery: не удалось открыть сокет")
		return nil
	}
	defer conn.Close()

	// Буфер побольше: ответы с полным набором scopes бывают крупными.
	_ = conn.SetWriteBuffer(64 * 1024)

	dst := &net.UDPAddr{IP: net.ParseIP("239.255.255.250"), Port: 3702}
	probe := wsdProbe(uuid.NewString())
	if _, err := conn.WriteToUDP(probe, dst); err != nil {
		log.Debug().Err(err).Msg("WS-Discovery: не удалось отправить Probe")
		return nil
	}

	deadline := time.Now().Add(window)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetReadDeadline(deadline)

	var out []passiveHit
	buf := make([]byte, 64*1024)
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			// Тайм-аут — обычное завершение: значит, больше никто не ответил.
			break
		}
		out = append(out, parseWSD(buf[:n])...)
	}
	return out
}

// ---------------------------------------------------------------------- mDNS

// mDNS-ответ, который нас интересует: адреса (A) и имена служб (PTR/SRV).
//
// Своя реализация вместо библиотеки: нужен один запрос и разбор ответа, а
// новая зависимость в проекте, который ставят на камеры и домашние
// серверы без интернета, — лишний риск при обновлении.
const (
	mdnsAddr = "224.0.0.251:5353"
	dnsA     = 1
	dnsPTR   = 12
	dnsSRV   = 33
)

// mdnsQueryNames — имена, которые спрашиваем.
//
// openipc.local отвечает A-записью: так камеры OpenIPC представляются в
// сети. Службы _onvif/_rtsp дают адреса камер других марок, которые
// публикуют себя в mDNS.
var mdnsQueryNames = []struct {
	name  string
	qtype uint16
}{
	{"openipc.local", dnsA},
	{"_onvif._tcp.local", dnsPTR},
	{"_rtsp._tcp.local", dnsPTR},
}

// discoverMDNS отправляет запросы и разбирает ответы.
func discoverMDNS(ctx context.Context, window time.Duration) []passiveHit {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		log.Debug().Err(err).Msg("mDNS: не удалось открыть сокет")
		return nil
	}
	defer conn.Close()

	dst, err := net.ResolveUDPAddr("udp4", mdnsAddr)
	if err != nil {
		return nil
	}
	for _, q := range mdnsQueryNames {
		if _, err := conn.WriteToUDP(buildDNSQuery(q.name, q.qtype), dst); err != nil {
			log.Debug().Err(err).Msg("mDNS: не удалось отправить запрос")
			return nil
		}
	}

	deadline := time.Now().Add(window)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetReadDeadline(deadline)

	var out []passiveHit
	buf := make([]byte, 9000)
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			break
		}
		for _, h := range parseMDNSResponse(buf[:n]) {
			if h.IP != "" {
				out = append(out, h)
			}
		}
	}
	return out
}

// buildDNSQuery собирает запрос из одного вопроса.
func buildDNSQuery(name string, qtype uint16) []byte {
	msg := make([]byte, 12, 128)
	binary.BigEndian.PutUint16(msg[4:], 1) // QDCOUNT
	for _, label := range strings.Split(strings.TrimSuffix(name, "."), ".") {
		if label == "" {
			continue
		}
		msg = append(msg, byte(len(label)))
		msg = append(msg, label...)
	}
	msg = append(msg, 0)
	msg = binary.BigEndian.AppendUint16(msg, qtype)
	msg = binary.BigEndian.AppendUint16(msg, 1) // QCLASS IN
	return msg
}

// parseMDNSResponse вытаскивает IPv4-адреса из ответа и связывает их с
// именами служб, если они есть в том же пакете.
//
// Имя службы (SRV) и адрес (A) часто лежат в разных пакетах, поэтому
// привязка неполная: адрес без имени тоже годится — по нему дальше
// работает обычный опрос камеры.
func parseMDNSResponse(raw []byte) []passiveHit {
	if len(raw) < 12 {
		return nil
	}
	qd := int(binary.BigEndian.Uint16(raw[4:]))
	an := int(binary.BigEndian.Uint16(raw[6:]))
	off := 12

	// Пропускаем вопросы.
	for i := 0; i < qd; i++ {
		_, next, ok := dnsReadName(raw, off)
		if !ok || next+4 > len(raw) {
			return nil
		}
		off = next + 4
	}

	names := map[string]string{} // имя записи → имя экземпляра службы
	type addr struct {
		ip  string
		own string
	}
	var addrs []addr

	for i := 0; i < an; i++ {
		owner, next, ok := dnsReadName(raw, off)
		if !ok || next+10 > len(raw) {
			break
		}
		rtype := binary.BigEndian.Uint16(raw[next:])
		rdlen := int(binary.BigEndian.Uint16(raw[next+8:]))
		rdata := next + 10
		if rdata+rdlen > len(raw) {
			break
		}

		switch rtype {
		case dnsA:
			if rdlen == 4 {
				ip := net.IPv4(raw[rdata], raw[rdata+1], raw[rdata+2], raw[rdata+3]).String()
				addrs = append(addrs, addr{ip: ip, own: owner})
			}
		case dnsPTR:
			if inst, _, ok := dnsReadName(raw, rdata); ok {
				names[inst] = inst
			}
		}
		off = rdata + rdlen
	}

	out := make([]passiveHit, 0, len(addrs))
	for _, a := range addrs {
		// Имя записи вида "OpenIPC-1234.local" — годится как подпись.
		name := strings.TrimSuffix(a.own, ".local")
		if n, ok := names[a.own]; ok {
			name = n
		}
		out = append(out, passiveHit{
			IP:      a.ip,
			Name:    name,
			Sources: []string{SourceMDNS},
		})
	}
	return out
}

// dnsReadName читает имя с учётом сжатия и возвращает его и смещение после.
//
// Сжатие (указатель 0xC0) встречается почти в каждом ответе mDNS, без его
// разбора ответ не читается вовсе.
func dnsReadName(msg []byte, off int) (string, int, bool) {
	var parts []string
	// next — смещение сразу за именем в исходном месте пакета. При
	// разборе указателя имя читается по другому адресу, но вернуться
	// нужно туда, откуда начали, иначе следующие записи прочитаются
	// со сдвигом.
	next := -1
	jumps := 0

	for {
		if off >= len(msg) {
			return "", 0, false
		}
		n := int(msg[off])
		switch {
		case n == 0:
			off++
			if next < 0 {
				next = off
			}
			return strings.Join(parts, "."), next, true
		case n&0xC0 == 0xC0:
			if off+1 >= len(msg) {
				return "", 0, false
			}
			if next < 0 {
				next = off + 2
			}
			ptr := int(binary.BigEndian.Uint16(msg[off:]) & 0x3FFF)
			if ptr >= len(msg) || jumps > 20 {
				return "", 0, false
			}
			jumps++
			off = ptr
		default:
			if off+1+n > len(msg) {
				return "", 0, false
			}
			parts = append(parts, string(msg[off+1:off+1+n]))
			off += 1 + n
		}
	}
}

// --------------------------------------------------------------------- peers

// peerList — ответ /api/v1/peers камеры OpenIPC.
//
// Список недоверенный: он приходит из сети и ведёт к запросам по адресам
// из него, поэтому принимаются только литеральные адреса без учётных
// данных в URL — как это делает и собственная страница камеры.
type peerList struct {
	Cameras []struct {
		Address  string `json:"address"`
		Name     string `json:"name"`
		URL      string `json:"url"`
		OpenIPC  bool   `json:"openipc"`
		QuietFor int64  `json:"quietFor"`
		Self     bool   `json:"self"`
	} `json:"cameras"`
}

// parsePeers превращает ответ камеры в находки.
func parsePeers(raw []byte) []passiveHit {
	var list peerList
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil
	}
	out := make([]passiveHit, 0, len(list.Cameras))
	for _, c := range list.Cameras {
		ip := strings.TrimSpace(c.Address)
		parsed := net.ParseIP(ip)
		// Только IPv4: дальше по адресу строится URL вида http://адрес/,
		// и для IPv6 понадобились бы квадратные скобки. Реестр OpenIPC
		// отдаёт IPv4, а всё остальное — уже чужая фантазия.
		if parsed == nil || parsed.To4() == nil || parsed.IsLoopback() {
			continue
		}
		// Имя в реестре percent-кодировано (пробелы как %20) — раскодируем,
		// иначе в интерфейсе будет «VIVOTEK%20FD9360-H».
		name := c.Name
		if decoded, err := url.PathUnescape(name); err == nil {
			name = decoded
		}
		out = append(out, passiveHit{
			IP:      ip,
			Name:    name,
			Sources: []string{SourcePeers},
			OpenIPC: c.OpenIPC,
		})
	}
	return out
}

// discoverPeers спрашивает реестр соседей у уже известных камер.
//
// Источник — только камеры нашего парка, сохранённые в базе: чужому
// устройству в сети такой запрос отправлять незачем, а своим он ничего
// не стоит. Обычно хватает одной камеры: она видит ту же подсеть.
func (s *CameraScanner) discoverPeers(ctx context.Context, window time.Duration) []passiveHit {
	if s.knownCameras == nil {
		return nil
	}

	cams, err := s.knownCameras(ctx)
	if err != nil {
		log.Debug().Err(err).Msg("реестр соседей: не удалось получить список камер")
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, window)
	defer cancel()

	// Больше пяти камер не опрашиваем: список соседей у них одинаковый,
	// а каждый запрос — это ожидание ответа медленного устройства.
	const maxSources = 5
	var out []passiveHit
	tried := 0

	for _, cam := range cams {
		if tried >= maxSources {
			break
		}
		ip := strings.TrimSpace(cam.IP)
		if net.ParseIP(ip) == nil {
			continue
		}
		tried++

		hits, err := s.fetchPeerList(ctx, ip)
		if err != nil {
			continue
		}
		out = append(out, hits...)
	}
	return out
}

// fetchPeerList запрашивает /api/v1/peers у одной камеры.
func (s *CameraScanner) fetchPeerList(ctx context.Context, ip string) ([]passiveHit, error) {
	// Свой HTTP-клиент с коротким тайм-аутом: камера, которая не ответила
	// за пару секунд, не должна задерживать скан.
	client := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("http://%s/api/v1/peers", ip), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// 204 — штатный ответ «в этой подсети соседей нет» или «вы не в своей
	// подсети». Это не ошибка, просто пусто.
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("реестр соседей ответил %d", resp.StatusCode)
	}

	// Ограничение размера: список соседей — это несколько килобайт, а
	// чужое устройство в сети может ответить чем угодно большим.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return nil, err
	}
	return parsePeers(body), nil
}

// mergePassiveAlive объединяет адреса из пассивного обнаружения с живыми.
//
// Через реестр соседей приходят адреса из других подсетей — их тоже
// опрашиваем: если камера стоит за другим шлюзом, но её видит своя же
// камера, до неё почти всегда доходит и сервер.
func mergePassiveAlive(idx *passiveIndex, alive []net.IP) []net.IP {
	seen := make(map[string]struct{}, len(alive))
	for _, ip := range alive {
		seen[ip.String()] = struct{}{}
	}
	for _, s := range idx.ips() {
		if _, ok := seen[s]; ok {
			continue
		}
		ip := net.ParseIP(s)
		if ip == nil {
			continue
		}
		seen[s] = struct{}{}
		alive = append(alive, ip)
	}
	return alive
}

// sourceCode возвращает код источника для интерфейса.
//
// Источников может быть несколько (адрес пришёл и по mDNS, и через реестр
// соседей) — тогда они перечисляются через запятую, чтобы оператор видел,
// откуда устройство известно.
func sourceCode(hit *passiveHit) string {
	if hit == nil || len(hit.Sources) == 0 {
		return SourceScan
	}
	return strings.Join(hit.Sources, ",")
}

// cameraFromPassive собирает карточку по объявлению устройства.
//
// Нужна для случая, когда устройство назвалось, но на запросы не ответило:
// без неё такая камера просто исчезала бы из списка, и оператор не понял
// бы, что она в сети есть. В карточке только то, что сообщило само
// устройство, — ничего додуманного.
func cameraFromPassive(ip string, hit *passiveHit) *domain.DiscoveredCamera {
	cam := &domain.DiscoveredCamera{IP: ip, Model: hit.Model}

	for _, src := range hit.Sources {
		switch src {
		case SourcePeers:
			// В реестре соседей бывают и чужие ONVIF-камеры: подписываем
			// OpenIPC только тех, кто сам так представился.
			if hit.OpenIPC {
				cam.Vendor = "openipc"
				cam.VendorName = "OpenIPC"
				cam.HowFound = SourcePeers
			}
		case SourceWSD:
			if cam.Vendor == "" && hit.ONVIFURL != "" {
				cam.Vendor = "onvif"
				cam.VendorName = "ONVIF"
				cam.HowFound = "onvif"
			}
		}
	}

	// Устройство ответило только по mDNS и не назвало ни марки, ни адреса
	// службы ONVIF: подписать его нечем, и в списке оно было бы строкой без
	// смысла. Такое отбрасываем — активный этап его всё равно проверит.
	if cam.Vendor == "" {
		return nil
	}
	return cam
}
