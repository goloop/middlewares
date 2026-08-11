# middlewares - довідник

Production-ready HTTP middleware для `net/http`, без сторонніх залежностей.
Кожен middleware - це `func(http.Handler) http.Handler`.

## Зміст

- [Композиція](#композиція)
- [RequestID](#requestid)
- [RealIP](#realip)
- [Recoverer](#recoverer)
- [Logger](#logger)
- [Timeout](#timeout)
- [Compress](#compress)
- [Throttle](#throttle)
- [RateLimit](#ratelimit)
- [CORS](#cors)
- [SecurityHeaders](#securityheaders)
- [MaxBytes](#maxbytes)
- [Нотатки дизайну](#нотатки-дизайну)

## Композиція

```go
h := middlewares.Chain(a, b, c)(handler) // a -> b -> c -> handler
h := middlewares.Handler(handler, a, b, c) // те саме, одним викликом
```

Перший middleware - найзовнішніший. Будь-який роутер, що приймає
`func(http.Handler) http.Handler` (зокрема `goloop/mux` через `Use`), приймає їх
напряму.

## RequestID

Призначає кожному запиту ідентифікатор, кладе його в контекст і повертає у
заголовку відповіді.

```go
middlewares.RequestID(
    middlewares.WithRequestIDHeader("X-Request-ID"),         // дефолт
    middlewares.WithRequestIDGenerator(func() string {...}), // дефолт: 128-біт random
    middlewares.WithTrustedRequestIDHeader(),                // приймати безпечні inbound
)

id := middlewares.RequestIDFrom(r.Context())
```

Вхідні ідентифікатори за замовчуванням ігноруються. З
`WithTrustedRequestIDHeader` inbound-значення приймається лише якщо це друкований
ASCII завдовжки до 128 байтів; інакше генерується новий.

## RealIP

Визначає IP клієнта в контекст, читається через `middlewares.RealIPFrom`.

```go
middlewares.RealIP(
    middlewares.WithTrustedProxies("10.0.0.0/8", "192.168.0.0/16"),
    middlewares.TrustLoopbackProxies(),
)
```

**Модель довіри до проксі.** За замовчуванням береться адреса прямого піра
(`RemoteAddr`), а forwarded-заголовки ігноруються - інакше будь-який клієнт міг
би підробити `X-Forwarded-For`. Заголовки враховуються лише коли `RemoteAddr`
належить довіреному діапазону: тоді перевага `X-Real-IP`, інакше найлівіший запис
`X-Forwarded-For`.

## Recoverer

Ловить panic, логує зі стек-трейсом і перетворює на відповідь.

```go
middlewares.Recoverer(
    middlewares.WithRecoverLogger(logger),  // дефолт: slog.Default
    middlewares.WithRecoverHandler(func(w http.ResponseWriter, r *http.Request, v any) {
        // власна сторінка/JSON помилки
    }),
)
```

**Recoverer не може змінити вже надіслану відповідь.** Якщо хендлер устиг
записати статус і тіло перед panic - Recoverer лише логує. На незакомічену
відповідь він запускає налаштований handler або надсилає простий `500`.
`http.ErrAbortHandler` перепаніковується, щоб сервер обірвав з'єднання як завжди.

## Logger

Один структурований рядок на запит через `log/slog`.

```go
middlewares.Logger(
    middlewares.WithLogger(logger),           // дефолт: slog.Default
    middlewares.WithLogLevel(slog.LevelInfo), // дефолт
    middlewares.WithLogMessage("http request"),
    middlewares.WithSkip(func(r *http.Request) bool { return r.URL.Path == "/health" }),
)
```

Атрибути: `method`, `path`, `status`, `bytes`, `duration`, а також `request_id`,
коли раніше відпрацював `RequestID`. Нічого не робиться, якщо запит пропущено або
логер вимкнено на рівні.

`WithContextLogger()` додатково кладе в контекст запитовий логер, що вже несе
`request_id`, `method`, `path` (і `remote_ip`, коли відпрацював `RealIP`), тож
рядки хендлерів корелюються з рядком запиту без того, щоб хтось пам'ятав про
ідентифікатори:

```go
h := middlewares.Logger(middlewares.WithContextLogger())
// у хендлері:
middlewares.LoggerFrom(r.Context()).Warn("upstream refused", "status", code)
```

`LoggerFrom` поза запитом чи без опції відкочується до `slog.Default()`, тож
місцю виклику ніколи не треба перевіряти. Опція - opt-in, бо алокує на запит.

## Timeout

Дедлайн на запит поверх `http.TimeoutHandler`.

```go
middlewares.Timeout(5*time.Second, middlewares.WithTimeoutMessage("timeout"))
```

По завершенні скасовує контекст і відповідає `503`. **Він не вбиває горутину
хендлера** - довгий хендлер має слухати `r.Context().Done()`, щоб реально
зупинитись. Відповідь по таймауту і записи хендлера синхронізовані
`http.TimeoutHandler`, тож гонки немає.

## Compress

Прозорий gzip для клієнтів, що його приймають.

```go
middlewares.Compress(
    middlewares.WithCompressLevel(gzip.DefaultCompression),
    middlewares.WithCompressMinBytes(1024), // дефолт
    middlewares.WithCompressTypes("text/html", "application/json", ...),
)
```

**Правила пропуску стиснення.** Відповідь не стискається, коли: клієнт не шле
`Accept-Encoding: gzip`; метод `HEAD`; статус `204`/`304`; вже задано
`Content-Encoding`; медіатип не в наборі стисних; медіатип `text/event-stream`;
або тіло менше за мінімум. Стиснена відповідь виставляє `Content-Encoding: gzip`,
прибирає `Content-Length` і додає `Vary: Accept-Encoding`. gzip-райтери
переюзуються через пул.

## Throttle

Внутрішньопроцесний обмежувач конкурентності.

```go
middlewares.Throttle(100,
    middlewares.WithThrottleStatus(http.StatusServiceUnavailable), // дефолт 503
    middlewares.WithThrottleRetryAfter(1),                         // секунди; 0 - без заголовка
)
```

Одночасно виконується не більше `limit` запитів; надлишкові одразу відхиляються,
а не ставляться в чергу. Це в межах процесу, не розподілений rate limiter.
`Throttle` панікує, якщо `limit` недодатний.

Він відповідає на інше питання, ніж `RateLimit`: скільки цієї роботи вміщається
в процес одночасно - проти того, як часто один клієнт може повертатися. Перед
дорогою роботою на кшталт хешування паролів доречні обидва.

## RateLimit

Як часто один викликач може повертатися, з ключем на ваш вибір.

```go
middlewares.RateLimit(middlewares.RateLimitConfig{
    Key:    middlewares.KeyByIP(),               // дефолт; або KeyByHeader("X-API-Key")
    Limit:  20,
    Window: 15 * time.Minute,
    Store:  middlewares.NewMemoryRateLimitStore(), // дефолт; у межах процесу
},
    middlewares.WithRateLimitStatus(429),          // дефолт
    // middlewares.WithFailOpen(),                 // спершу прочитайте нижче
)
```

Відхилений запит отримує налаштований статус, `Retry-After` і заголовки
`X-RateLimit-Limit`/`-Remaining`/`-Reset`; дозволений - ті самі заголовки без
`Retry-After`, тож клієнт може сповільнитись до того, як його відхилять.

Memory-стор - це token bucket, тому немає подвійного сплеску на межі вікон,
який дозволяє fixed window. Він рахує лише те, що бачив цей процес: за
балансувальником кожен інстанс тримає власні відра, і ліміт, який має діяти
на всіх, потребує спільного `RateLimitStore`. Інтерфейс - один метод, тож
реалізація на Redis чи базі - короткий шматок коду застосунку, а не залежність
цього пакета.

Коли стор не може відповісти, запит відхиляється з 503. Лімітер зазвичай
охороняє вхід, скидання пароля й одноразові коди, і збій стора, який мовчки
зняв би ліміт саме з цих маршрутів, перетворив би проблему доступності на
проблему безпеки. `WithFailOpen` явно обирає інший компроміс для маршрутів,
де досяжність важливіша.

Усе, що ключується адресою клієнта, успадковує застереження `RealIP`: за
проксі без політики довірених проксі всі клієнти резолвляться в проксі, і
ліміт стає одним відром на всіх. `RateLimit` панікує, якщо `Limit` чи
`Window` недодатні.

## CORS

Cross-Origin Resource Sharing, безпечний за замовчуванням.

```go
middlewares.CORS(
    middlewares.WithAllowedOrigins("https://app.example.com"), // або "*"
    middlewares.WithAllowedMethods("GET", "POST"),
    middlewares.WithAllowedHeaders("Authorization", "Content-Type"),
    middlewares.WithExposedHeaders("X-Total-Count"),
    middlewares.WithAllowCredentials(),
    middlewares.WithMaxAge(600),
)
```

Нічого не дозволено, доки ви не назвете origin. Preflight `OPTIONS` з дозволеного
origin отримує `204`. З credentials повертається конкретний origin (ніколи `*`).
Wildcard-origin разом із credentials - некоректна комбінація і **панікує при
створенні**.

## SecurityHeaders

Консервативний набір заголовків, без перезапису того, що вже виставив хендлер.

```go
middlewares.SecurityHeaders(
    middlewares.WithFrameOptions("DENY"),          // дефолт
    middlewares.WithReferrerPolicy("no-referrer"), // дефолт
    middlewares.WithHSTS(31536000),                // opt-in, лише HTTPS
    middlewares.WithCSP("default-src 'self'"),     // opt-in
)
```

Дефолти: `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`,
`Referrer-Policy: no-referrer`. HSTS і CSP - opt-in, бо залежать від деплою.

### Повна HSTS-політика

`WithHSTS(seconds)` видає лише `max-age`. Справжня політика майже завжди хоче
більшого, і його несе `WithHSTSPolicy`:

```go
middlewares.SecurityHeaders(middlewares.WithHSTSPolicy(middlewares.HSTS{
    MaxAge:            365 * 24 * time.Hour,
    IncludeSubDomains: true,
    Preload:           true,
}))
```

`preload` видається лише разом із `includeSubDomains`: preload-списки браузерів
інакше його відхиляють, а заголовок, який ніколи не буде вшанований, гірший за
чесний. Нульовий `MaxAge` пропускає заголовок цілком.

## MaxBytes

```go
middlewares.MaxBytes(1 << 20) // обмежити тіло запиту до 1 МіБ
```

Загортає тіло запиту в `http.MaxBytesReader`.

## Нотатки дизайну

- Внутрішня обгортка response-writer відстежує статус і кількість байтів та
  прокидає `Flush`, `Hijack` і `ReadFrom` через `http.ResponseController`, тож
  стрімінг, upgrade-и й ефективна віддача файлів продовжують працювати.
- Runtime-middleware ніколи не повертають помилок; помилки конфігурації
  (wildcard CORS із credentials, недодатний ліміт throttle) панікують при
  створенні, показуючи помилку розробника одразу.
