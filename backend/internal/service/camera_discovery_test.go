package service

import (
	"context"
	"encoding/binary"
	"net"
	"os"
	"testing"
)

// Sample ответа WS-Discovery: разбор недоверенного XML проверяется без сети.
const wsdSample = `<?xml version="1.0" encoding="UTF-8"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope"
 xmlns:wsd="http://schemas.xmlsoap.org/ws/2005/04/discovery"
 xmlns:wsa="http://schemas.xmlsoap.org/ws/2004/08/addressing">
 <SOAP-ENV:Body>
  <wsd:ProbeMatches>
   <wsd:ProbeMatch>
    <wsa:EndpointReference><wsa:Address>urn:uuid:11111111-2222-3333-4444-555555555555</wsa:Address></wsa:EndpointReference>
    <wsd:Types>dn:NetworkVideoTransmitter</wsd:Types>
    <wsd:Scopes>onvif://www.onvif.org/type/video_encoder onvif://www.onvif.org/name/Вход%20во%20двор onvif://www.onvif.org/hardware/GK7205V500</wsd:Scopes>
    <wsd:XAddrs>http://192.168.1.50/onvif/device_service</wsd:XAddrs>
   </wsd:ProbeMatch>
  </wsd:ProbeMatches>
 </SOAP-ENV:Body>
</SOAP-ENV:Envelope>`

func TestParseWSD(t *testing.T) {
	hits := parseWSD([]byte(wsdSample))
	if len(hits) != 1 {
		t.Fatalf("ожидалась одна находка, получено %d", len(hits))
	}
	h := hits[0]
	if h.IP != "192.168.1.50" {
		t.Errorf("неверный адрес: %q", h.IP)
	}
	if h.Name != "Вход во двор" {
		t.Errorf("имя из scopes разобрано неверно: %q", h.Name)
	}
	if h.Model != "GK7205V500" {
		t.Errorf("модель из scopes разобрана неверно: %q", h.Model)
	}
	if h.ONVIFURL != "http://192.168.1.50/onvif/device_service" {
		t.Errorf("адрес службы ONVIF: %q", h.ONVIFURL)
	}
	if len(h.Sources) != 1 || h.Sources[0] != SourceWSD {
		t.Errorf("источник находки: %v", h.Sources)
	}
}

// TestParseWSDRejectsBrokenXML: битый ответ не должен ронять скан.
func TestParseWSDRejectsBrokenXML(t *testing.T) {
	if hits := parseWSD([]byte("<not-xml")); len(hits) != 0 {
		t.Errorf("битый ответ дал находки: %v", hits)
	}
}

// TestParseWSDIgnoresHostnames: имя вместо адреса не годится — по нему
// пришлось бы резолвить имя, а объявления из сети недоверенные.
func TestParseWSDIgnoresHostnames(t *testing.T) {
	raw := `<e:Envelope xmlns:e="x"><e:Body><d:ProbeMatches xmlns:d="y"><d:ProbeMatch>
<d:XAddrs>http://camera.local/onvif/device_service</d:XAddrs>
</d:ProbeMatch></d:ProbeMatches></e:Body></e:Envelope>`
	if hits := parseWSD([]byte(raw)); len(hits) != 0 {
		t.Errorf("имя хоста принято за адрес: %v", hits)
	}
}

// dnsName кодирует имя для DNS-пакета в тестах.
func dnsName(name string) []byte {
	var out []byte
	start := 0
	for i := 0; i <= len(name); i++ {
		if i == len(name) || name[i] == '.' {
			label := name[start:i]
			if label != "" {
				out = append(out, byte(len(label)))
				out = append(out, label...)
			}
			start = i + 1
		}
	}
	return append(out, 0)
}

// buildMDNSResponse собирает ответ с A-записью и PTR, указывающим на неё
// сжатым именем, — так это выглядит в реальной сети.
func buildMDNSResponse() []byte {
	var msg []byte
	header := make([]byte, 12)
	binary.BigEndian.PutUint16(header[2:], 0x8400) // ответ, авторитетный
	binary.BigEndian.PutUint16(header[6:], 2)      // ANCOUNT
	msg = append(msg, header...)

	nameOff := len(msg)
	msg = append(msg, dnsName("OpenIPC-1234.local")...)
	// A: 192.168.1.50
	msg = binary.BigEndian.AppendUint16(msg, dnsA)
	msg = binary.BigEndian.AppendUint16(msg, 1)
	msg = binary.BigEndian.AppendUint32(msg, 120)
	msg = binary.BigEndian.AppendUint16(msg, 4)
	msg = append(msg, 192, 168, 1, 50)

	// PTR "_onvif._tcp.local" → "OpenIPC-1234.local" (сжатый указатель).
	msg = append(msg, dnsName("_onvif._tcp.local")...)
	msg = binary.BigEndian.AppendUint16(msg, dnsPTR)
	msg = binary.BigEndian.AppendUint16(msg, 1)
	msg = binary.BigEndian.AppendUint32(msg, 120)
	ptr := []byte{0xC0, byte(nameOff)}
	msg = binary.BigEndian.AppendUint16(msg, uint16(len(ptr)))
	msg = append(msg, ptr...)
	return msg
}

func TestParseMDNSResponse(t *testing.T) {
	hits := parseMDNSResponse(buildMDNSResponse())
	if len(hits) != 1 {
		t.Fatalf("ожидалась одна находка, получено %d", len(hits))
	}
	if hits[0].IP != "192.168.1.50" {
		t.Errorf("неверный адрес: %q", hits[0].IP)
	}
	if hits[0].Name != "OpenIPC-1234.local" {
		t.Errorf("имя службы разобрано неверно: %q", hits[0].Name)
	}
}

// TestDNSReadNameCompression проверяет разбор указателя: без него ответ
// mDNS не читается, потому что имена в нём почти всегда сжаты.
func TestDNSReadNameCompression(t *testing.T) {
	// "local" лежит с начала пакета, второе имя ссылается на него
	// указателем — так mDNS и экономит место в ответе.
	msg := dnsName("local") // смещение 0
	openipcOff := len(msg)
	// Метка без завершающего нуля: имя продолжается указателем, а не
	// заканчивается — иначе указатель оказался бы уже за концом имени.
	msg = append(msg, 7)
	msg = append(msg, "OpenIPC"...)
	ptrOff := len(msg)
	msg = append(msg, 0xC0, 0x00)

	// Читаем с начала первого имени: оно продолжается указателем на «local».
	name, next, ok := dnsReadName(msg, openipcOff)
	if !ok {
		t.Fatal("имя со сжатием не разобрано")
	}
	if name != "OpenIPC.local" {
		t.Errorf("неверное имя: %q", name)
	}
	if next != ptrOff+2 {
		t.Errorf("имя со сжатием: ожидалось смещение %d, получено %d", ptrOff+2, next)
	}
}

func TestParsePeers(t *testing.T) {
	raw := []byte(`{"cameras":[
		{"address":"192.168.1.10","name":"Вход","openipc":true,"quietFor":0,"self":false},
		{"address":"192.168.1.11","openipc":false,"quietFor":12,"self":false},
		{"address":"","self":true},
		{"address":"камера-дома"},
		{"address":"127.0.0.1"}
	]}`)

	hits := parsePeers(raw)
	if len(hits) != 2 {
		t.Fatalf("ожидалось 2 пригодных адреса, получено %d: %v", len(hits), hits)
	}
	if hits[0].IP != "192.168.1.10" || hits[0].Name != "Вход" {
		t.Errorf("первая находка: %+v", hits[0])
	}
	if len(hits[0].Sources) != 1 || hits[0].Sources[0] != SourcePeers {
		t.Errorf("источник находки: %v", hits[0].Sources)
	}
}

func TestMergePassiveAlive(t *testing.T) {
	idx := newPassiveIndex()
	idx.add(passiveHit{IP: "192.168.1.10", Sources: []string{SourceWSD}})
	idx.add(passiveHit{IP: "192.168.2.20", Sources: []string{SourcePeers}})

	alive := []net.IP{net.ParseIP("192.168.1.10"), net.ParseIP("192.168.1.30")}
	merged := mergePassiveAlive(idx, alive)

	if len(merged) != 3 {
		t.Fatalf("ожидалось 3 адреса, получено %d: %v", len(merged), merged)
	}
	// Дубль не должен появиться второй раз.
	count := 0
	for _, ip := range merged {
		if ip.String() == "192.168.1.10" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("адрес продублирован: %v", merged)
	}
}

func TestSourceCode(t *testing.T) {
	if got := sourceCode(nil); got != SourceScan {
		t.Errorf("без пассивной находки ожидался scan, получено %q", got)
	}
	h := &passiveHit{Sources: []string{SourceMDNS, SourcePeers}}
	if got := sourceCode(h); got != "mdns,peers" {
		t.Errorf("несколько источников: %q", got)
	}
}

func TestCameraFromPassive(t *testing.T) {
	// Только mDNS без адреса службы: подписывать нечем.
	if cam := cameraFromPassive("192.168.1.10", &passiveHit{Sources: []string{SourceMDNS}}); cam != nil {
		t.Errorf("ожидалось отсутствие карточки, получено %+v", cam)
	}
	// Реестр соседей с флагом OpenIPC — камера OpenIPC.
	cam := cameraFromPassive("192.168.1.10", &passiveHit{
		Sources: []string{SourcePeers}, Model: "GK7205V500", OpenIPC: true})
	if cam == nil || cam.Vendor != "openipc" || cam.HowFound != SourcePeers {
		t.Errorf("карточка из реестра: %+v", cam)
	}
	// WS-Discovery с адресом службы — ONVIF.
	cam = cameraFromPassive("192.168.1.11", &passiveHit{
		Sources: []string{SourceWSD}, ONVIFURL: "http://192.168.1.11/onvif/device_service"})
	if cam == nil || cam.Vendor != "onvif" {
		t.Errorf("карточка из WS-Discovery: %+v", cam)
	}
}

// TestDiscoveryLive — проверка на живой сети стенда.
//
// По умолчанию пропускается: в CI и на машине без камер в той же подсети
// находок не будет, и тест стал бы нестабильным. На стенде запускается так:
//
//	NVR_LIVE_DISCOVERY=1 go test ./internal/service -run TestDiscoveryLive -v
func TestDiscoveryLive(t *testing.T) {
	if os.Getenv("NVR_LIVE_DISCOVERY") == "" {
		t.Skip("живая проверка отключена: задайте NVR_LIVE_DISCOVERY=1")
	}

	wsd := discoverWSD(context.Background(), discoveryWindow)
	mdns := discoverMDNS(context.Background(), discoveryWindow)

	t.Logf("WS-Discovery: %d находок, mDNS: %d находок", len(wsd), len(mdns))
	for _, h := range wsd {
		t.Logf("  wsd: %s %q %q", h.IP, h.Name, h.Model)
	}
	for _, h := range mdns {
		t.Logf("  mdns: %s %q", h.IP, h.Name)
	}
	if len(wsd) == 0 && len(mdns) == 0 {
		t.Log("ничего не найдено: возможно, камеры в другой подсети или запрещён multicast")
	}
}
