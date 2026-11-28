import QtQuick
import QtQuick.Controls.Basic
import Nvr 1.0

// Ячейка стены: одна камера.
//
// Пока потока нет, показываем снимок кадра, а не чёрный прямоугольник:
// оператор должен видеть, что камера жива, даже если декодер ещё
// поднимается или поток оборвался.
Rectangle {
    id: cell

    property int cellIndex: 0
    property string cameraId: ""
    property bool selected: false
    /**
     * В камере этой ячейки сейчас тревога.
     *
     * Признак ставит стена по событию из потока, и снимает его по таймеру:
     * ячейка — единственное место, где тревога видна вместе с картинкой,
     * и по ней сразу понятно, куда смотреть.
     */
    property bool alarmed: false

    /** Имя камеры для OSD. Обновляется при загрузке списка камер. */
    property string cameraTitle: ""
    /** Время сервера одной строкой; обновляется раз в секунду. */
    property string clockText: ""
    /**
     * Настройки наложения этой камеры (карточка камеры на сервере).
     *
     * Значения по умолчанию повторяют прежний вид: имя, время и рамки в
     * левом верхнем углу. Настройка приходит вместе со списком камер.
     */
    property var osd: ({
        enabled: true, show_name: true, show_time: true, show_boxes: true,
        position: "tl", x: 0.02, y: 0.02, size: "normal"
    })
    /**
     * Рамки детекций из потока событий.
     *
     * Массив {x,y,w,h} в долях кадра и время истечения. Рамка живёт
     * несколько секунд после события: одиночный кадр на видео заметить
     * невозможно, а событие приходит раз в секунду.
     */
    property var alarmBoxes: []

    // Сигналы названы не так, как свойство: одноимённые свойство и сигнал
    // конфликтуют — Qt берёт свойство, и вызов «selected()» перестаёт быть
    // функцией (видели эту ошибку в журнале на стенде).
    signal cellClicked()
    signal cellDoubleClicked()
    /** Правая кнопка: меню действий с ячейкой (открыть на весь экран, очистить). */
    signal cellMenuRequested()

    color: "#0d1117"
    border.width: cell.alarmed ? 3 : (cell.selected ? 2 : 1)
    border.color: cell.alarmed ? "#e05252" : (cell.selected ? "#2196f3" : "#1f2733")

    StreamPlayer {
        id: player
    }

    Component.onCompleted: {
        console.log("ячейка", cell.cellIndex, "готова")
        cell.updateTitle()
        cell.refreshClock()
    }

    // Субпоток: в сетке 4×4 основной поток не поднять на 16 ячеек.
    //
    // Адрес берём у сервера (через prepareStream): он знает внешний номер
    // канала камеры и проверяет право на просмотр. Счётчик streamsRevision
    // в выражении нужен, чтобы адрес пересчитался, когда ответ придёт.
    readonly property string streamUrl: {
        var revision = Api.streamsRevision
        return cell.cameraId.length > 0 ? Api.streamUrl(cell.cameraId, true) : ""
    }

    onCameraIdChanged: {
        if (cell.cameraId.length > 0) {
            Api.prepareStream(cell.cameraId)
        }
        cell.updateTitle()
    }

    /** Имя камеры и настройки наложения: список приходит с сервера. */
    function updateTitle() {
        var cam = cell.cameraId.length > 0 ? Api.camera(cell.cameraId) : null
        cell.cameraTitle = cell.cameraId.length > 0 ? Api.cameraName(cell.cameraId) : ""
        cell.osd = cell.normalizeOsd(cam ? cam.osd : null)
    }

    /**
     * Приводит настройки OSD к полному набору.
     *
     * Сервер может не прислать поле (старая запись) или прислать не все —
     * подставляем значения по умолчанию, чтобы ячейка не осталась без
     * наложения и не падала на undefined.
     */
    function normalizeOsd(raw) {
        var d = {
            enabled: true, show_name: true, show_time: true, show_boxes: true,
            position: "tl", x: 0.02, y: 0.02, size: "normal"
        }
        if (!raw) {
            return d
        }
        var keys = ["enabled", "show_name", "show_time", "show_boxes",
                    "position", "x", "y", "size"]
        for (var i = 0; i < keys.length; ++i) {
            if (raw[keys[i]] !== undefined && raw[keys[i]] !== null) {
                d[keys[i]] = raw[keys[i]]
            }
        }
        return d
    }

    function pad2(value) {
        return value < 10 ? "0" + value : "" + value
    }

    /**
     * Время сервера в его поясе.
     *
     * Поправка часов и смещение пояса приходят из потока событий
     * (см. LiveEvents): часы дежурной машины могут отставать, а метка
     * на видео должна совпадать с метками событий и архива.
     */
    function refreshClock() {
        var d = new Date(Date.now() + Live.serverOffsetMs
                         + Live.serverTzOffsetSec * 1000)
        cell.clockText = pad2(d.getUTCDate()) + "." + pad2(d.getUTCMonth() + 1)
            + "." + d.getUTCFullYear() + " "
            + pad2(d.getUTCHours()) + ":" + pad2(d.getUTCMinutes()) + ":"
            + pad2(d.getUTCSeconds())
    }

    /** Добавляет рамку из события; координаты переводим в доли кадра. */
    function addAlarmBox(event) {
        var b = event.bbox
        var fw = Number(event.frame_w)
        var fh = Number(event.frame_h)
        if (!b || !(fw > 0) || !(fh > 0)) {
            return
        }
        var list = cell.alarmBoxes.slice()
        list.push({
            x: Number(b.x) / fw,
            y: Number(b.y) / fh,
            w: Number(b.w) / fw,
            h: Number(b.h) / fh,
            cls: event.object_class || "",
            expires: Date.now() + 2500
        })
        // Больше двадцати рамок одновременно оператор всё равно не различит.
        if (list.length > 20) {
            list = list.slice(list.length - 20)
        }
        cell.alarmBoxes = list
    }

    /** Убирает рамки, чьи события уже не свежие. */
    function pruneBoxes() {
        if (cell.alarmBoxes.length === 0) {
            return
        }
        var now = Date.now()
        var kept = []
        for (var i = 0; i < cell.alarmBoxes.length; ++i) {
            if (cell.alarmBoxes[i].expires > now) {
                kept.push(cell.alarmBoxes[i])
            }
        }
        if (kept.length !== cell.alarmBoxes.length) {
            cell.alarmBoxes = kept
        }
    }

    Timer {
        interval: 1000
        running: true
        repeat: true
        onTriggered: cell.refreshClock()
    }

    Timer {
        interval: 500
        running: true
        repeat: true
        onTriggered: cell.pruneBoxes()
    }

    Connections {
        target: Api
        function onCamerasChanged() {
            cell.updateTitle()
        }
    }

    Connections {
        target: Live
        function onEventReceived(event) {
            // Рамки нужны только этой камере. Остальные события (тревоги,
            // звук, СКУД) обрабатывает стена.
            if (event.type === "detection" && event.camera_id === cell.cameraId) {
                cell.addAlarmBox(event)
            }
        }
    }

    onStreamUrlChanged: {
        if (cell.streamUrl.length > 0) {
            // Запись в журнал: по ней на дежурной машине видно, дошла ли
            // до ячейки команда открыть поток, — иначе «нет видео» ничем
            // не отличается от «камера не назначена».
            console.log("ячейка", cell.cellIndex, "камера", cell.cameraId, "поток открывается")
            player.start(cell.streamUrl)
        } else {
            player.stop()
        }
    }

    // При закрытии окна конвейеры надо остановить: иначе процесс остаётся
    // с занятыми декодерами и сокетами — на стенде это выглядит как
    // «клиент закрыт, а камеры не отпускает».
    Component.onDestruction: player.stop()

    VideoItem {
        id: video
        anchors.fill: parent
        visible: player.active
    }

    // Рамки детекций поверх видео.
    //
    // Координаты событий заданы в пикселях кадра камеры, а элемент рисует
    // кадр с полями от «вписать с сохранением пропорций». Перевод делает
    // VideoItem.normalizedRect — та же формула, что и в самой отрисовке,
    // поэтому рамка не разъезжается с картинкой.
    Repeater {
        model: cell.alarmBoxes
        delegate: Rectangle {
            readonly property var mapped: {
                // Обращение к свойствам нужно для пересчёта при смене
                // размера ячейки или кадра: вызов функции сам по себе
                // зависимость в QML не создаёт.
                var cellW = video.width
                var cellH = video.height
                var frameW = video.frameWidth
                var frameH = video.frameHeight
                if (!cellW || !cellH || !frameW || !frameH) {
                    return Qt.rect(0, 0, 0, 0)
                }
                return video.normalizedRect(modelData.x, modelData.y,
                                            modelData.w, modelData.h)
            }
            x: mapped.x
            y: mapped.y
            width: mapped.width
            height: mapped.height
            visible: player.active && cell.osd.enabled && cell.osd.show_boxes
                     && width > 0 && height > 0
            color: "transparent"
            border.width: 2
            radius: 2
            // Цвет как в веб-интерфейсе: человек и транспорт различаются
            // с одного взгляда.
            border.color: modelData.cls === "person" ? "#34c759" : "#0a84ff"
        }
    }

    // Кадры приходят сигналом: элемент отрисовки рисует их в потоке сцены,
    // а плеер выдаёт из потока GStreamer.
    Connections {
        target: player
        function onFrameReady(image) {
            video.setFrame(image)
        }
        function onStatusChanged() {
            // Причина отказа должна остаться в журнале: на стенде туда
            // смотрят, когда в ячейке пусто.
            if (player.status.length > 0) {
                console.warn("ячейка", cell.cellIndex, "камера", cell.cameraId,
                             "поток:", player.status)
            }
        }
    }

    Image {
        anchors.fill: parent
        visible: !player.active && cell.cameraId.length > 0
        source: cell.cameraId.length > 0 ? Api.snapshotUrl(cell.cameraId) : ""
        fillMode: Image.PreserveAspectCrop
        // Кэш выключен: снимок нужен свежий, иначе ячейка «замерзает».
        cache: false
    }

    Label {
        anchors.centerIn: parent
        visible: cell.cameraId.length === 0
        text: qsTr("Ячейка %1").arg(cell.cellIndex + 1)
        color: "#4a5768"
    }

    // Причина, по которой камеры нет: чаще всего её удалили на сервере,
    // а в сохранённой раскладке она осталась. Показываем словами — иначе
    // ячейка просто чёрная, и непонятно, что делать.
    Label {
        anchors.centerIn: parent
        width: parent.width - 16
        visible: cell.cameraId.length > 0 && !player.active
                 && Api.cameraError(cell.cameraId).length > 0
        text: Api.cameraError(cell.cameraId)
        color: "#e05252"
        font.pixelSize: 12
        wrapMode: Text.WordWrap
        horizontalAlignment: Text.AlignHCenter
    }

    // Состояние подключения: причина видна словами, а не только по картинке.
    Label {
        anchors.left: parent.left
        anchors.bottom: parent.bottom
        anchors.margins: 6
        visible: player.status.length > 0
        text: player.status
        color: "#ffb300"
        font.pixelSize: 11
        wrapMode: Text.WordWrap
        width: parent.width - 12
    }

    // Мигание при тревоге. Три вспышки, а не постоянное свечение:
    // движущаяся картинка в соседних ячейках должна оставаться видна,
    // а заметность тревоги обеспечивает именно начало.
    Rectangle {
        id: alarmFlash
        anchors.fill: parent
        color: "#e05252"
        opacity: 0
        visible: cell.alarmed

        SequentialAnimation {
            running: alarmFlash.visible
            loops: 3
            NumberAnimation {
                target: alarmFlash
                property: "opacity"
                from: 0.45
                to: 0
                duration: 420
            }
            PauseAnimation {
                duration: 220
            }
        }
    }

    Label {
        anchors.right: parent.right
        anchors.top: parent.top
        anchors.margins: 6
        visible: cell.alarmed
        text: qsTr("ТРЕВОГА")
        color: "#ff8a80"
        font.bold: true
        font.pixelSize: 11
    }

    // OSD поверх видео: имя камеры и время сервера.
    //
    // Рисуется клиентом, а не вжигается в поток: перекодировать шестнадцать
    // потоков ради надписи положило бы машину, а рамки и текст на клиенте
    // остаются чёткими при любом разрешении.
    Rectangle {
        id: osdBox
        readonly property real margin: 4
        readonly property bool custom: cell.osd.position === "custom"
        readonly property int fontSize: cell.osd.size === "small" ? 10
                                        : (cell.osd.size === "large" ? 16 : 11)

        x: custom ? parent.width * cell.osd.x
                  : ((cell.osd.position === "tr" || cell.osd.position === "br")
                     ? parent.width - width - margin : margin)
        y: custom ? parent.height * cell.osd.y
                  : ((cell.osd.position === "bl" || cell.osd.position === "br")
                     ? parent.height - height - margin : margin)
        visible: cell.cameraId.length > 0 && cell.osd.enabled
                 && (cell.osd.show_name || cell.osd.show_time)
        color: "rgba(0,0,0,0.45)"
        radius: 3
        implicitWidth: osdColumn.implicitWidth + 10
        implicitHeight: osdColumn.implicitHeight + 4

        Column {
            id: osdColumn
            anchors.centerIn: parent
            spacing: 0

            Label {
                visible: cell.osd.show_name
                text: cell.cameraTitle
                color: "#fff"
                font.pixelSize: osdBox.fontSize
                font.bold: true
            }
            Label {
                visible: cell.osd.show_time
                text: cell.clockText
                color: "#fff"
                font.pixelSize: osdBox.fontSize
                font.family: "monospace"
            }
        }
    }

    MouseArea {
        anchors.fill: parent
        // Правую кнопку принимаем здесь же: очистка ячейки должна быть
        // осознанным действием через меню, а не тем же нажатием, что
        // и открытие камеры на весь экран.
        acceptedButtons: Qt.LeftButton | Qt.RightButton
        onClicked: function (mouse) {
            if (mouse.button === Qt.RightButton) {
                cell.cellMenuRequested()
                return
            }
            cell.cellClicked()
        }
        onDoubleClicked: cell.cellDoubleClicked()
    }
}
