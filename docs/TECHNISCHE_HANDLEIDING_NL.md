# Cockpit — Technische handleiding

**Nederlandse editie · versie 1.0 · 23 oktober 2026 · Intern**

Deze handleiding beschrijft hoe Cockpit in de huidige repository werkt. Hij is bedoeld voor ontwikkelaars, technisch beheerders en supportmedewerkers die de applicatie moeten begrijpen, uitrollen of aanpassen. De broncode blijft leidend. API-paden, permissies, statussen, foutcodes en veldnamen staan bewust in het Engels; dat voorkomt verwarring bij het lezen van de code en het testen van requests.

# 1. Systeemoverzicht

Cockpit is een webapplicatie met een JSON-API voor de operationele uitvoering van orders. De belangrijkste onderdelen zijn:

- orders, doeldata, toegewezen medewerkers, holds, QC-beslissingen en audithistorie;
- picklijsten, productmanuals, werksessies en dagdoelen;
- signalering van werk, workload, kwaliteit en doorvoer;
- corrigerende en preventieve acties (CAPA);
- orderautomatiseringen;
- systeemintegraties (SI), projecten en projectchecklists;
- het SWI-procesbord, escalaties en externe synchronisatietaken;
- scannen op ordernummer, gegenereerde orderbarcodes en offline scanherstel;
- verwerking van laptop-prep-logs;
- live verversing in de browser via server-sent events (SSE).

## 1.1 Applicatie in beeld

```text
Browser of native WebView
        |
        | same-origin HTTP, JSON, SSE en statische bestanden
        v
Go net/http-server
        |
        +---- authenticatie en aanvullende afdelingspermissies
        +---- domeinvalidatie en transacties
        +---- SSE, rate limits, QR-login en clientsessies in geheugen
        +---- automation- en escalatieworkers
        |
        v
SQLite-database in WAL-modus

Externe integratieworker, indien gebruikt
        |
        +---- claimt en rondt external_sync_jobs af via de API
        +---- AFAS, Omnitracker, Intune, Knox en Apple Business Manager
```

De server bevat geen directe netwerkconnectors voor deze externe systemen. Cockpit plant en bewaart synchronisatietaken. Een aparte integratieworker moet ze via de API ophalen en afmelden.

## 1.2 Verdeling van de code

- `pkg/order`, `pkg/si`, `pkg/swi`, `pkg/capa` en `pkg/automation` bevatten domeinmodellen en validatie.
- `pkg/db` verzorgt SQL, transacties, migraties en queryprojecties.
- `pkg/api` bevat routing, authenticatie-middleware, requestverwerking, responses, SSE en autorisatie op objectniveau.
- `frontend` bevat statische HTML, CSS, fonts en gewone JavaScript. Voor de hoofdapplicatie is geen browserbuild nodig.
- `client` is een compacte WebView-client met machinegebonden pairing.
- SQLite is de leidende gegevensbron. SSE-berichten zijn signalen om opnieuw op te halen, geen tweede eventstore.

# 2. Overzicht van de repository

| Pad | Functie |
|---|---|
| `main.go` | Leest configuratie, opent de database, maakt de bootstrap-admin aan, start server en workers en handelt een gecontroleerde shutdown af. |
| `pkg/api/server.go` | Registreert routes en gedeeld API-gedrag. |
| `pkg/api/middleware.go` | Authenticatie, permissiecontroles, logging, panic recovery en een response-wrapper die streaming ondersteunt. |
| `pkg/api/security.go` | Securityheaders, same-origincontroles voor schrijfacties, client-IP-afhandeling en fixed-window rate limits. |
| `pkg/api/session.go` | Keuze tussen Bearer- en cookietoken en het beheer van de sessiecookie. |
| `pkg/api/clientguard.go` | Client-onlymodus, pairing, launch-bewijzen, beheer van clientsleutels en QR-pairing. |
| `pkg/api/qrlogin.go` | Kortlevende tickets voor QR-login tussen browser en telefoon. |
| `pkg/api/*_handlers.go`, `handlers.go`, `handlers_holds.go` | Handlers per functioneel onderdeel. |
| `pkg/api/events.go` | SSE-abonnementen in geheugen, publicatie, heartbeats en streamauthenticatie. |
| `pkg/auth` | Rollen, permissies, PBKDF2-wachtwoordhashes, willekeurige tokens en tokendigests. |
| `pkg/order` | Orderstatussen, hold-normalisatie, SLA-berekening, order-DTO’s en manual-DTO’s. |
| `pkg/attention` | Deterministische projectie voor My Work en attention. |
| `pkg/mention` | Leest en resolveert `@username`-vermeldingen. |
| `pkg/capa` | CAPA-statussen, overgangen en validatie. |
| `pkg/automation` | Triggers, acties en regelvalidatie voor automations. |
| `pkg/si` | SI-statussen, configuratie, projecten en statusovergangen. |
| `pkg/swi` | Processtappen, taken, escalaties, synchronisatieplannen en validatie. |
| `pkg/db/db.go` | Basisschema, openen van SQLite en additieve migraties. |
| `pkg/db/store.go`, `pkg/db/*.go` | Transacties en queries voor de verschillende aggregates. |
| `frontend/index.html` | Shell van de modulaire browserapplicatie. |
| `frontend/js` | Applicatie- en paginamodules, scanner, pairing, QR-goedkeuring en offlineondersteuning. |
| `frontend/styles.css` | Styling van de hoofdapplicatie. |
| `frontend/sw.js` | Serviceworker voor de applicatieshell en offline navigatie. |
| `frontend/build.py`, `frontend/build.ps1` | Genereren de oudere single-filevariant. |
| `frontend/cockpit-standalone.html` | Gegenereerde singlefilevariant; niet de primaire applicatie. |
| `frontend/test/test.html` | Browsertests. |
| `client/main.go` | Native WebView-launcher en lokaal pairingbestand. |
| `test.sh` | Liveregressietests voor de HTTP-API. |

# 3. Starten en configureren

## 3.1 Bouwen en starten

Start de server vanuit de repositoryroot. De statische bestanden worden uit de relatieve map `frontend` geserveerd en zitten niet in het Go-binary.

```bash
go build -o cockpit-server .
PORT=8080 DB_PATH=cockpit.db ADMIN_PASSWORD='replace-me' ./cockpit-server
```

De standaardpoort is `80`. Op Unix-achtige systemen kan daarvoor een extra rechtenaanpassing nodig zijn. Gebruik voor ontwikkeling bij voorkeur een hoge poort.

Een expliciete productiestart ziet er bijvoorbeeld zo uit:

```bash
PORT=8080 \
DB_PATH=/var/lib/cockpit/cockpit.db \
ADMIN_USERNAME=admin \
ADMIN_PASSWORD='replace-with-a-long-random-secret' \
COCKPIT_PUBLIC_URL=https://cockpit.example.com \
COCKPIT_TRUST_PROXY=1 \
./cockpit-server
```

Zet `COCKPIT_TRUST_PROXY` alleen op `1` als clients de proxyheaders niet zelf kunnen vervalsen en de reverse proxy deze headers correct overschrijft.

## 3.2 Omgevingsvariabelen

| Variabele | Standaard | Betekenis |
|---|---:|---|
| `PORT` | `80` | HTTP-poort. De server spreekt zelf plain HTTP; gebruik een proxy voor TLS. |
| `DB_PATH` | `cockpit.db` | Pad naar de SQLite-database. |
| `SLA_TARGET_HOURS` | `24` | Standaard-SLA voor nieuwe orders zonder expliciete doeldatum. Ongeldige of niet-positieve waarden vallen terug op 24. |
| `SESSION_HOURS` | `24` | Levensduur van een loginsessie. Ongeldige of niet-positieve waarden vallen terug op 24. |
| `ADMIN_USERNAME` | `admin` | Gebruikersnaam van de bootstrap-admin. |
| `ADMIN_PASSWORD` | leeg | Wachtwoord van de bootstrap-admin. Bestaat het account nog niet en is geen wachtwoord gezet, dan logt de server eenmalig een willekeurig wachtwoord. |
| `COCKPIT_TRUST_PROXY` | `0` | Bij `1` vertrouwt de server onder andere `X-Real-IP`, `X-Forwarded-For` en de ondersteunde forwarded host/protocolheaders. |
| `COCKPIT_PUBLIC_URL` | leeg | Externe basis-URL voor QR- en pairinglinks en voor de effectieve origin. |
| `COCKPIT_CLIENT_ONLY` | `0` | Bij `1` is een gepairde client vereist voor niet-vrijgegeven UI- en statische paden. |
| `COCKPIT_CLIENT_KEYS` | `cockpit-client-keys.json` | JSON-bestand met paired-clientsleutels. Dit bestand staat buiten SQLite. |
| `SWI_AUTO_ESCALATE_MINUTES` | `15` | Interval voor automatische SWI-escalatie. `0` schakelt de worker uit; ongeldige of negatieve waarden vallen terug op 15. |
| `AUTOMATION_INTERVAL_SECONDS` | `60` | Interval voor orderautomatisering. `0` schakelt de worker uit; ongeldige of negatieve waarden vallen terug op 60. |
| `COCKPIT_SERVER` | standaard van de client | Alleen voor de native client: overschrijft de server-URL voordat het opgeslagen pairingbestand wordt gelezen. |

## 3.3 Volgorde bij het opstarten

1. Lees de configuratie en activeer afhandeling van `SIGINT` en `SIGTERM`.
2. Open SQLite, voer het basisschema en de additieve migraties uit en ping de database.
3. Zorg dat de bootstrap-admin bestaat. Bestaande accounts worden bij het opstarten niet overschreven.
4. Initialiseer eventueel de clientguard en het JSON-sleutelbestand.
5. Bouw de API-server, SSE-hub en rate limiters op en maak waar nodig de boot-pairingcode aan.
6. Start de workers voor automatische escalatie en automations. De automationworker draait direct een eerste evaluatie en daarna volgens de ticker.
7. Start HTTP met timeouts van 5 seconden voor het lezen van requestheaders, 15 seconden voor lezen, 15 seconden voor schrijven en 60 seconden voor idle verbindingen. SSE heft zijn write deadline expliciet op.
8. Bij shutdown accepteert de server geen nieuw werk meer en is er maximaal 10 seconden voor een nette HTTP-shutdown.

`GET /healthz` geeft `{"status":"ok"}` terug. Dit is een procescheck, geen diepgaande controle van de database of externe integraties.

# 4. HTTP-stack en API-afspraken

## 4.1 Verwerking van een request

De middleware staat effectief in deze volgorde:

```text
panic recovery
  -> requestlogging
    -> securityheaders
      -> optionele paired-clientguard
        -> same-origincontrole voor schrijvende browserrequests
          -> router op methode en pad
            -> sessieauthenticatie, indien geregistreerd
              -> permissie-middleware, indien geregistreerd
                -> validatie en objectautorisatie in de handler
```

Routes gebruiken Go 1.22+-patronen zoals `GET /api/v1/orders/{id}`. Statische bestanden vallen onder `/`; een onbekend `/api/`-pad geeft een JSON-404.

HTML en `/` krijgen `Cache-Control: no-cache, must-revalidate`. API-responses krijgen `no-store` en `Pragma: no-cache`.

## 4.2 Sessiegegevens in een request

Afgeschermde routes accepteren:

```http
Authorization: Bearer <session-token>
```

of de cookie `cockpit_session`. Als beide aanwezig zijn, gaat de Bearer-token voor. Sessietokens worden niet via queryparameters geaccepteerd.

## 4.3 JSON en responses

- Successvolle API-responses zijn JSON-objecten. De inhoud zit meestal onder een duidelijke sleutel, bijvoorbeeld `order`, `orders`, `detail`, `action` of `session`.
- JSON-requestbody’s mogen maximaal 1 MiB groot zijn. Data na de eerste JSON-waarde wordt geweigerd.
- Pad-ID’s zijn decimale gehele getallen en moeten bij de gevraagde resource passen.
- Velden van het type `time.Time` worden als RFC 3339-strings teruggegeven. In SQLite staan de meeste tijdstippen als UTC Unix-milliseconden.
- Een Go `time.Duration`, zoals SLA `remaining`, wordt als geheel aantal nanoseconden geserialiseerd.
- Bulkacties geven per item `ok`, `code` en `message` terug. Een fout bij één item betekent niet automatisch dat alle andere items zijn mislukt.

Foutresponses hebben deze vorm:

```json
{
  "error": {
    "code": "invalid_transition",
    "message": "human-readable detail"
  }
}
```

| HTTP | Betekenis |
|---:|---|
| 400 | Ongeldige JSON, ID, enum of veldwaarde. |
| 401 | Sessiegegevens ontbreken, zijn ongeldig, ingetrokken of verlopen. |
| 403 | De gebruiker is aangemeld maar heeft geen permissie, faalt op objectautorisatie of op een pairing-/origincontrole. |
| 404 | Resource, order, barcode of route is niet gevonden. |
| 409 | Conflict in het domein, bijvoorbeeld een ongeldige overgang, hold, stale write, dubbele waarde, ontbrekende QC of idempotentieconflict. |
| 410 | Het QR-loginticket is verlopen. |
| 422 | De upload is geen herkenbaar prep-logboek. |
| 429 | Rate limit bereikt; relevante handlers geven ook `Retry-After` terug. |
| 500 | Onverwachte database- of serverfout. |

Veelgebruikte machinale codes zijn `not_found`, `duplicate_order_number`, `duplicate_username`, `duplicate_product_code`, `duplicate_manual`, `duplicate_customer_number`, `duplicate_project_code`, `duplicate_si_code`, `invalid_status`, `invalid_transition`, `active_hold`, `hold_active`, `hold_already_resolved`, `qc_not_passed`, `stale_write`, `scan_idempotency_conflict`, `invalid_capa`, `invalid_capa_transition`, `invalid_automation_rule` en `rate_limited`.

# 5. Authenticatie, sessies en autorisatie

## 5.1 Wachtwoorden en login

Wachtwoorden worden opgeslagen in deze vorm:

```text
pbkdf2-sha256$600000$<hex-salt>$<hex-derived-key>
```

Elk wachtwoord heeft een willekeurige salt van 16 bytes en een derived key van 32 bytes met PBKDF2-SHA256 en 600.000 iteraties. De vergelijking van de digest gebeurt in constante tijd. Bij een onbekende gebruikersnaam voert de server ook een hashverificatie uit, zodat het tijdsverschil kleiner wordt.

Login gebruikt process-lokale fixed-window limits:

- 20 loginrequests per client-IP per minuut;
- 5 pogingen per combinatie van IP en genormaliseerde gebruikersnaam per vijf minuten;
- een geslaagde login reset de bucket voor dat account.

Bij een geslaagde login maakt de server 32 willekeurige bytes aan, hex-geëncodeerd voor transport. Voor nieuwe sessies staat alleen de SHA-256-digest van de token in `api_tokens`. De token verloopt na `SESSION_HOURS`. Logout trekt de gebruikte sessie in; een wachtwoordreset trekt alle sessies van die gebruiker in.

De loginresponse bevat de gebruiker en de ruwe token en zet ook `cockpit_session`. Deze cookie is host-only, `HttpOnly`, `SameSite=Strict` en `Secure` als de effectieve requestscheme HTTPS is. API-clients kunnen in plaats daarvan de Bearer-token gebruiken.

De modulaire frontend bewaart bij een wachtwoordlogin de token in local storage voor de API-helper. Bij QR-login gebruikt de frontend juist de cookiesessie en verwijdert hij de lokale Bearer-token. De backend ondersteunt dus HttpOnly-cookies, maar niet iedere frontendlogin is uitsluitend cookiegebaseerd.

Voor nieuwe gebruikers en wachtwoordresets geldt het beleid uit `pkg/api/security.go`: 10 tot 256 tekens, niet gelijk aan de getrimde gebruikersnaam (hoofdletterongevoelig), met minstens één letter en één cijfer of symbool. Bij login wordt alleen het bestaande wachtwoord geverifieerd.

## 5.2 QR-login

QR-login is een ander mechanisme dan native-clientpairing:

1. Een browser zonder sessie vraagt `GET /api/v1/qr-login` aan.
2. De server maakt een kortlevend ticket in geheugen en geeft een QR-data-URL en approval-URL terug.
3. Een telefoon met een geldige sessie opent de approvalpagina en kiest goedkeuren of afwijzen via `POST /api/v1/qr-login/approve`.
4. Bij goedkeuring maakt de server een normale databasesessie voor diezelfde gebruiker.
5. De oorspronkelijke browser pollt het ticket. Na goedkeuring ontvangt hij de sessiecookie en het gebruikersprofiel en kan hij verder.

Tickets verlopen en blijven niet bewaard na een herstart. Op de bijbehorende endpoints zitten rate limits. Iedere aangemelde gebruiker mag een ticket goedkeuren of afwijzen. Er ontstaat geen extra rechtenniveau: de browser krijgt exact de permissies van de goedkeurende gebruiker.

## 5.3 Rollen

| Rol | Basiscapaciteiten |
|---|---|
| `viewer` | Orderlijst, orderdetail en audit, comments lezen, SI en processen bekijken en notificaties lezen. |
| `operator` | Viewer plus orders aanmaken en laten overgaan, holds, comments plaatsen, scannen, picklijsten en manuals uitvoeren en processen voortzetten of escaleren. |
| `qc` | Orders en audit bekijken, orderovergangen, QC indienen, comments, manuals beheren, SI en processen bekijken en processen escaleren. |
| `npi` | SI aanmaken, wijzigen, laten overgaan en aan projecten koppelen; gebruikers en processen bekijken; proces- en integratietaakbeheer. |
| `sc` | Orders en audit bekijken, comments, manuals beheren en goedkeuren, klanten beheren, SI en gebruikers bekijken en processen bekijken. |
| `admin` | Alle permissies, inclusief gebruikers, afdelingen, backup, pairing, automations en geforceerd procesbeheer. |

De server controleert permissies ook als de frontend een knop verbergt.

## 5.4 Afdelingen en aanvullende permissies

Een gebruiker behoudt zijn rolpermissies en krijgt daarnaast de permissies van alle afdelingen waaraan hij is gekoppeld. Afdelingsrechten zijn additief: ze kunnen een recht toevoegen, maar geen rolrecht afnemen.

```text
rol heeft permissie OF minstens één gekoppelde afdeling kent permissie toe
```

Wijzigen van afdelingspermissies vereist `users:manage`. De frontend krijgt de aanvullende permissies mee, maar de server controleert ieder beschermd request opnieuw.

## 5.5 Autorisatie op objectniveau

Een routepermissie is niet altijd voldoende:

- starten, bijwerken en beëindigen van werk kan alleen door de orderassignee of een admin;
- CAPA-wijzigingen vereisen QC, of de CAPA-owner met aanvullend `orders:transition`;
- een owner zonder QC mag geen titel, foutclassificatie, owner of due date wijzigen;
- CAPA aanmaken en definitief verifiëren vereist `qc:submit`;
- geforceerd voortzetten van SWI vereist daarnaast `process:manage`;
- beheerhandlers controleren bovendien bestaande resources en onderlinge relaties.

# 6. Orders, holds, QC en SLA

## 6.1 Ordergegevens

Een order heeft een uniek ordernummer, status, doel- en auditdatums en kan daarnaast een assignee, gepauzeerde seconden en operationele gegevens bevatten:

- debiteur-/klantnummer en klantnaam;
- Omnitracker-ticket;
- device en assetnummer;
- configuratie;
- gekoppeld SI-ID;
- gegenereerde barcode.

Nieuwe orders beginnen op `Received`. Zonder expliciete doeldatum gebruikt de server `SLA_TARGET_HOURS`. Een opgegeven doeldatum moet geldig zijn en in de toekomst liggen. Aanmaken en muteren schrijft audithistorie onder de aangemelde gebruikersnaam. Een door de client meegegeven `performedBy` vervangt in de normale HTTP-flow die actor niet.

## 6.2 Statusovergangen

| Huidige basisstatus | Toegestane doelen |
|---|---|
| `Received` | `Processing`, `QC_Review` |
| `Processing` | `QC_Review`, `Received` |
| `QC_Review` | `Completed`, `Processing`, `Received` |
| `Completed` | geen |

`On_Hold` is een filter- en weergaveclassificatie, geen direct overgangsdoel. Een client kan er niet zelf naar schakelen; een hold verandert de opgeslagen status zoals hieronder beschreven.

Voor `Completed` gelden twee aparte voorwaarden:

1. de statusgrafiek moet `QC_Review -> Completed` toestaan;
2. de laatste QC-check moet bestaan en `PASS` zijn.

Een latere `FAIL` blokkeert afronding dus totdat er een nieuwere `PASS` is. Voltooide orders weigeren verdere overgangen, holds en QC-submissions.

## 6.3 Optimistic concurrency

Orderovergangen en het plaatsen van een hold accepteren `expectedUpdatedAt`. Als dit veld is meegegeven, moet het exact overeenkomen met de actuele `updated_at`. Anders volgt `stale_write` met HTTP 409. Zo kan een client geen beslissing toepassen op een versie die hij niet heeft gezien. Zonder dit veld vindt die vergelijking niet plaats.

## 6.4 Holds

Een hold plaatsen:

- is niet toegestaan op een voltooide order of een order met al een actieve hold;
- vereist een reden;
- slaat de status op als `Held_` plus de basisstatus, bijvoorbeeld `Held_Processing`;
- voegt in dezelfde transactie een onopgeloste hold en auditregel toe.

Zolang een hold actief is, zijn normale overgangen geblokkeerd. Bij het oplossen:

- krijgt de hold een `resolved_at` en de naam van degene die hem oplost;
- verdwijnt het voorvoegsel `Held_` en komt de basisstatus terug;
- wordt de holdduur in hele seconden opgeteld bij `paused_seconds`;
- verschijnt een auditregel.

Een al opgeloste hold geeft `hold_already_resolved`. Bulk resolve geeft het resultaat per order terug.

## 6.5 SLA-berekening

De SLA wordt bij het lezen berekend en niet als vaste status opgeslagen. De effectieve huidige tijd wordt naar achteren verschoven met:

- de opgetelde `paused_seconds` van opgeloste holds;
- de verstreken tijd sinds de oudste actieve hold.

Daarna geldt `remaining = targetCompletionAt - effectiveNow`.

| SLA | Regel |
|---|---|
| `BREACHED` | De resterende tijd is negatief. |
| `WARNING` | De doeldatum is nog niet verstreken en er resteren maximaal vier uur. |
| `ON_TIME` | Er resteren meer dan vier uur. |

De response bevat de oorspronkelijke doeldatum en de resterende tijd met teken. Attention en automations gebruiken dezelfde pauzecorrectie.

## 6.6 QC-records

QC-checks worden alleen toegevoegd en bevatten `PASS` of `FAIL`, inspector-ID, notities, fail reason, een snapshot van de technician/assignee en een tijdstip. Bij `FAIL` is in de HTTP-validatie een fail reason verplicht. Een QC-submission verandert de orderstatus niet automatisch; een client of geautoriseerde workflow voert die overgang apart uit.

De QC-API biedt orderhistorie, een werkqueue met ordercontext, fail reasons en maandstatistieken voor pass/fail, trends, redenen, gebruikers en historie.

## 6.7 Audittrail

Ordermutaties schrijven `audit_logs` met order-ID, actie, actor en tijdstip. Manualantwoorden en flags, comments, holds, QC, toewijzingen, CAPA en statusovergangen dragen hieraan bij. Er is geen reguliere API om een afzonderlijke auditregel te wijzigen of te verwijderen.

# 7. Attention, werk, comments, notificaties en cijfers

## 7.1 Attentionprojectie

`GET /api/v1/attention` berekent een actuele projectie uit niet-voltooide orders en actieve holds. Per order verschijnt hooguit één kaart, met deze prioriteit:

| Prioriteit | Reden | Ernst | Voorwaarde |
|---:|---|---|---|
| 1 | `ON_HOLD` | high | Minstens één actieve hold. |
| 2 | `SLA_BREACHED` | high | De gecorrigeerde doeldatum is verstreken. |
| 3 | `SLA_WARNING` | medium | De gecorrigeerde doeldatum ligt binnen vier uur. |
| 4 | `AWAITING_QC` | low | De basisstatus is `QC_Review`. |
| 5 | `FRESH` | low | Een `Received`-order is minder dan twee uur oud. |

Kaarten worden eerst op ernst en daarna op doeldatum gesorteerd. De tellers in de samenvatting tellen de gekozen reden per kaart, niet iedere voorwaarde die tegelijk waar is.

## 7.2 Comments en mentions

Een comment bevat auteur-ID en -naam, body en tijdstip. De mentionparser herkent `@username` buiten code spans met backticks. Letters, cijfers, `_`, `.` en `-` zijn toegestaan. Dubbele namen worden verwijderd en alleen bestaande gebruikers worden geresolveerd.

Een mention maakt een notificatie voor de genoemde gebruiker, behalve bij een self-mention. Comment, mentionkoppelingen, notificaties en auditregel worden in één transactie vastgelegd. De body wordt als tekst opgeslagen; bij het renderen escaped de frontend de inhoud in plaats van hem als vertrouwde HTML te behandelen.

## 7.3 Notificaties

Notificaties horen bij een gebruikersnaam en hebben een type, optionele order- en referentie-ID’s, body, aanmaaktijd en leestijd. Bronnen zijn onder andere mentions, manualflags, toewijzingen, CAPA, automations, escalaties en dagdoelen. Ophalen en als gelezen markeren werkt op de notificaties van de aangemelde gebruiker. SSE-notificaties zijn eveneens per ontvanger gefilterd.

## 7.4 Werksessies en klantworkflows

Een werksessie bevat order, gebruiker, begin- en eindtijd, huidige stap, een JSON-payload met checks en een completed-vlag. Per order kan slechts één actieve sessie bestaan. Starten en wijzigen vereist de assignee of een admin; op een voltooide order kan niet worden ingeklokt.

Een klantworkflow bepaalt de stappen die de medewerker ziet:

- één tot acht stappen;
- unieke, machineleesbare stapsleutels;
- maximaal 12 checks per stap;
- optionele pick- en QC-markeringen en metadata voor een volgende orderstatus.

Lezen van een workflow vereist `orders:list`, wijzigen `users:manage`. Het opslaan van werkvoortgang voert op zichzelf geen orderovergang uit.

## 7.5 Dagdoelen en operationele statistiek

Dagdoelen zijn gekoppeld aan een datum en gebruikersnaam. Gebruikers met QC-permissie kunnen een doel opslaan en als in-appnotificatie versturen. Verzenden zet een verzendtijdstip; er wordt geen e-mail verstuurd.

De operationele statistiek combineert aangemaakte en afgeronde orders, klokactiviteit, gemiddelde afhandeltijd, leaderboard, open orders en SLA-verdeling. De standaardperiode is 14 UTC-dagen en kan van 1 tot 60 dagen worden ingesteld.
# 8. Scannen, barcodes en offline herstel

Cockpit heeft twee scanpaden die bewust gescheiden zijn.

## 8.1 Scannen op ordernummer

`POST /api/v1/scan` trimt `code` en zoekt daarmee exact op ordernummer. Een code is verplicht en mag maximaal 256 tekens bevatten. De enige ondersteunde actie is `LOOKUP`; dat is ook de standaard. Bij een match geeft de API de order, de actuele detail- en SLA-projectie en het vastgelegde scanevent terug.

Voor betrouwbaar offline opnieuw versturen zijn deze velden beschikbaar:

- `clientEventId`: een optionele client-ID van maximaal 128 tekens, met letters, cijfers, `.`, `_`, `:` of `-`;
- `scannedAt`: het oorspronkelijke scantijdstip, niet vóór 2000-01-01 en maximaal vijf minuten in de toekomst;
- `receivedAt`: het moment waarop de server het event daadwerkelijk heeft ontvangen.

Voor een niet-lege clientevent-ID zorgt een partiële unieke index op `(scanned_by, client_event_id)` voor idempotentie per gebruiker. Opnieuw versturen met dezelfde order, code en actie geeft het bestaande event als duplicate terug. Wordt dezelfde ID gebruikt voor een andere order, code of actie, dan volgt `scan_idempotency_conflict`. De oorspronkelijk opgeslagen scan- en ontvangsttijden blijven behouden.

Een onbekend ordernummer geeft 404 `no_match` en maakt geen scanevent aan.

## 8.2 Gegenereerde orderbarcodes

De barcode-endpoints gebruiken het aparte veld `orders.barcode`:

- `POST /api/v1/orders/{id}/barcode` geeft de bestaande waarde terug of maakt een `CPT-…`-waarde aan. Met `regenerate` wordt een vervanger gemaakt.
- `GET /api/v1/orders/{id}/barcode` leest de huidige waarde zonder er een aan te maken.
- `GET /api/v1/scan/barcode?code=...` resolveert de barcode, schrijft een `barcode_scans`-regel met scantype en User-Agent en geeft order-, scan- en Omnitrackerlinks terug.
- `GET /api/v1/orders/{id}/barcode/history` geeft die barcodehistorie terug.

Deze barcodeevents staan los van de idempotente `scan_events` die voor offline scannen worden gebruikt.

## 8.3 Offline browser scanner

De offline scanner bewaart per gebruiker maximaal 500 items in local storage onder `cockpit_offline_scans_v1:<username>`. Elk item krijgt waar mogelijk een UUID en behoudt zijn oorspronkelijke `scannedAt`.

Bij synchronisatie:

- stuurt de client code, clientevent-ID en oorspronkelijk tijdstip naar `/api/v1/scan`;
- verwijdert hij een item pas na acceptatie door de server én een geslaagde lokale queue-update;
- probeert hij netwerkfouten, 408, 429 en 5xx opnieuw met exponentiële backoff tot vijf minuten;
- markeert hij permanente client- of domeinfouten als failed, zodat de gebruiker ze kan hervatten of weggooien;
- pauzeert hij waar nodig bij gewijzigde authenticatie of permissies;
- draait hij periodiek en bij online- en login-events opnieuw.

De serviceworker cachet alleen de statische shell, geen `/api/`-responses. Navigatie is network-first met een cached HTML-fallback. Andere statische GET-requests op dezelfde origin zijn cache-first en worden na een geslaagd networkrequest aangevuld. Zo blijft de scannershell offline bruikbaar zonder oude API-data als actueel te presenteren.

# 9. Productmanuals en checklists

## 9.1 Manualtemplates bij een product

Een product bevat een herbruikbare manualtemplate met unieke code, naam, omschrijving, afdeling, eventuele approvalmetadata en geordende blokken. Een blok heeft een volgnummer, titel, body en optionele assignee. Een assignee moet als gebruiker bestaan.

De template-API ondersteunt aanmaken, wijzigen en verwijderen, snelle aanmaak uit stapteksten, goedkeuren, afdelingswijzigingen en het bewerken, verwijderen en verplaatsen van blokken. Verplaatsen voorbij het begin of einde geeft `manual_edge`.

Goedkeuring is metadata met permissie `manuals:approve`. Algemeen templatebeheer gebruikt `manuals:manage`.

## 9.2 Manualinstances op een order

Bij het koppelen van een product aan een order kopieert Cockpit de productidentiteit en de toen bestaande blokken naar `order_manuals` en `order_manual_blocks`. Latere templatewijzigingen passen bestaande instances niet aan. Hetzelfde product kan niet twee keer aan dezelfde order worden gekoppeld. Zolang instances naar een product verwijzen, kan dat product niet zomaar worden verwijderd.

Elk blok in een instance ondersteunt:

- `YES`: beantwoord en als afgerond geteld;
- `NO`: beantwoord en als failed geteld;
- `CLEAR`: verwijdert het antwoord en een eventuele flag.

Iedere antwoordwijziging schrijft `manual_tick_log` en de orderaudit. Een manager kan een beantwoord blok met een verplichte reden flaggen of de flag verwijderen. Daarbij krijgt de medewerker die heeft geantwoord een notificatie en worden manualog en audit bijgewerkt. Opnieuw antwoorden of clearen verwijdert de flag eveneens.

Het verwijderen van een ordermanual ruimt de gekopieerde blokken en logregels op en schrijft een auditregel. De order en de overige audithistorie blijven bestaan.

## 9.3 De vijf soorten checklistgegevens

Deze modellen hebben elk een eigen functie en mogen niet door elkaar worden gehaald:

| Model | Gebruik |
|---|---|
| `checklist_items` | Picklijstregels bij een order, af- en uitvinken via de picklist-API. |
| `order_manual_blocks` | Gekopieerde YES/NO-stappen in een ordermanual. |
| `project_checklist_items` | Checklistregels bij een SI-project. |
| `process_tasks` | Verplichte en optionele taken die bij een SWI-stap worden aangemaakt. |
| `work_sessions.checks` | JSON-voortgang binnen een klantworkflow. |

Permissies, historie en levenscyclus verschillen per model.

# 10. CAPA

CAPA registreert corrigerende acties bij een order, eventueel gekoppeld aan een specifieke mislukte QC-check.

## 10.1 Velden en aanmaak

Een corrective action bevat order- en QC-referenties, titel, fail reason, owner, due date, root cause, action plan, resolution, status, maker, tijdstippen en verificatiegegevens. Aanmaken vereist `qc:submit`, een bestaande order en owner, een titel van maximaal 160 tekens en een due date in de toekomst. De lange tekstvelden zijn begrensd op 4.000 tekens.

## 10.2 Statussen

| Huidige status | Toegestane vervolgstatus |
|---|---|
| `OPEN` | `IN_PROGRESS`, `PENDING_VERIFICATION` |
| `IN_PROGRESS` | `OPEN`, `PENDING_VERIFICATION` |
| `PENDING_VERIFICATION` | `IN_PROGRESS`, `CLOSED` via verificatie |
| `CLOSED` | geen; de actie is immutable |

Voor `PENDING_VERIFICATION` moeten root cause, action plan en resolution ingevuld zijn. Een normale update kan niet rechtstreeks `CLOSED` zetten. Alleen `POST /api/v1/capa/{id}/verify` sluit een actie en daarvoor is een verification note verplicht.

QC-gebruikers kunnen classificatie en ownership wijzigen. Een owner zonder QC kan alleen de uitvoeringsvelden wijzigen en heeft daarvoor ook `orders:transition` nodig. Wijzigingen maken CAPA-events, orderaudit, relevante notificaties en SSE-signalen aan.

De lijst kan filteren op status, owner, `owner=me`, overdue en limiet. De samenvatting telt iedere status en daarnaast de niet-gesloten acties waarvan de due date is verstreken.

# 11. Orderautomatiseringen

Automationbeheer vereist `users:manage`.

## 11.1 Regels

| Trigger | Kandidaten |
|---|---|
| `QC_FAILED` | Mislukte QC-checks die op of na het aanmaken van de regel zijn ontstaan. |
| `SLA_BREACHED` | Niet-voltooide orders waarvan de gecorrigeerde SLA is overschreden. |
| `HOLD_AGING` | Actieve holds die ouder zijn dan `thresholdMinutes`. |

| Actie | Resultaat |
|---|---|
| `NOTIFY` | Maakt een automationnotificatie voor iedere targetgebruiker. |
| `ASSIGN` | Wijst de order toe aan precies één target, schrijft toewijzingshistorie en stuurt die gebruiker een notificatie. |
| `ESCALATE` | Maakt een SWI-escalatie op `L1`, `L2` of `L3`, eventueel gericht aan één gebruiker. |

Een holddrempel ligt tussen 1 en 525.600 minuten. Andere triggers moeten drempel nul hebben. Regelnamen en targets worden gevalideerd, targets moeten bestaande gebruikers zijn en dubbele targets worden geweigerd. Een bericht mag maximaal 500 tekens bevatten.

De escalatiereden volgt uit de trigger: quality bij QC, SLA breach bij SLA en blocked bij hold aging.

## 11.2 Uitvoering en deduplicatie

De achtergrondworker, het handmatige evaluate-endpoint en de directe evaluatie na een `FAIL`-QC gebruiken dezelfde servermutex en store-evaluator. Een run wordt eerst als `PENDING` geclaimd met een unieke combinatie van regel en triggersleutel. Daarna wordt de actie uitgevoerd en de run `SUCCESS` of `FAILED`.

De triggersleutels zijn:

- `qc:<qc-check-id>`;
- `hold:<hold-id>`;
- `sla:<order-id>`.

Er is daardoor één geregistreerde poging per regel en kandidaat. Een mislukte run blijft failed en komt niet automatisch als nieuwe kandidaat terug. Wijzigen van een regel verwijdert eerdere sleutels niet; een nieuwe regel krijgt een nieuw ID.

Geslaagde runs publiceren waar relevant automation-, order- en notification-SSE-signalen. Runregels verwijzen met `ON DELETE CASCADE` naar regel en order. Verwijderen van een van die parents verwijdert dus ook de bijbehorende runhistorie.

# 12. SI: klanten, projecten, configuraties en checklists

SI-records beschrijven systeemintegraties, los van de uitvoering van een order.

## 12.1 Structuur

```text
Klant
  -> Project
       -> projectchecklist
       -> gekoppelde SI-records

SI-record
  -> primair project en aanvullende projectkoppelingen
  -> JSON-configuratie
  -> status, versie en omgeving
  -> SI-events
```

Klanten hebben een uniek nummer en kunnen een coördinator hebben. Projectcodes zijn uniek binnen de klant. Een SI-code heeft de vorm `SI-{debit}-{device}-{config}`. De configuratie kan onder andere device type, Windows-profiel, software, assetsticker, sleeve, screenprotector, werkinstructies, SWI-, workflow- en QC-profielen, automations en escalation flow bevatten.

## 12.2 SI-levenscyclus

| Huidige status | Toegestane doelen |
|---|---|
| `Concept` | `In Validation`, `Blocked` |
| `In Validation` | `Accepted`, `Concept`, `Blocked` |
| `Accepted` | `Outphased`, `Blocked` |
| `Blocked` | `Concept`, `In Validation`, `Accepted` |
| `Outphased` | geen |

De omgevingen zijn `TEST`, `ACCEPTATIE` en `PRODUCTIE`. Versies en statusevents vormen de revisie- en statushistorie. Projectkoppelingen en projectchecklists zijn apart van orderpicklijsten en SWI-taken.

# 13. SWI-proces, escalaties en synchronisatie

De SWI-laag is een tweede, gedetailleerdere toestandsmachine bij een order. Hij vervangt de basisorderstatus niet. Een processresponse bevat een voorgestelde basisstatus, maar het voortzetten van het proces voert die orderovergang niet impliciet uit.

## 13.1 Stappen en overgangen

```text
OrderImport
  <-> WorkPreparation
        <-> PointingInWork
              <-> InControlWork
                    <-> ProcessManagement -> Completed

Operationele stappen kunnen naar Escalation.
Na het oplossen van de laatste escalatie keert het proces
terug naar de vastgelegde return stage.
```

Concreet geldt:

- `OrderImport` gaat naar `WorkPreparation`;
- `WorkPreparation` gaat naar `PointingInWork` of terug naar `OrderImport`;
- `PointingInWork` gaat naar `InControlWork` of terug naar `WorkPreparation`;
- `InControlWork` gaat naar `ProcessManagement` of terug naar `PointingInWork`;
- `ProcessManagement` gaat naar `Completed` of terug naar `InControlWork`;
- `Completed` is terminal;
- escalation is beschikbaar vanuit operationele stappen en blokkeert normale voortgang zolang die actief is.

De domein- en storegrafiek bevat `ProcessManagement -> Completed`, maar de huidige HTTP-validatie `ValidateAdvance` weigert ieder terminal doel voordat de store wordt aangeroepen. Daardoor kan `POST /api/v1/orders/{id}/process/advance` een proces via dit requestpad nu niet op `Completed` zetten, ondanks de aanwezige edge in de onderliggende grafiek.

## 13.2 Staptaken en geforceerd voortzetten

Bij het binnengaan van een stap maakt Cockpit de bijbehorende tasktemplates eenmalig aan. Taken hebben volgorde, titel, omschrijving, rol, required-vlag, afhandelaar en afrondtijd. Normaal moeten alle verplichte taken in de huidige stap zijn afgerond voordat het proces verder kan.

Een request met `force=true` omzeilt alleen de controle op verplichte taken en vereist naast `process:advance` ook `process:manage`. De statusgrafiek en de blokkade bij een actieve escalatie blijven gelden.

Het procesbord groepeert orders in lanes en toont taak- en synctellers, volgende stappen en procesgegevens. Escaleerd werk komt eerst, daarna wordt op tijd in de huidige stap gesorteerd. De metrics bevatten onder andere stapverdeling, open taken, pending en failed syncs, actieve leeftijd en recente afronding.

## 13.3 Escalaties

De niveaus zijn `L1`, `L2` en `L3`. Redenen zijn `SLA_BREACH`, `STAGE_STALLED`, `BLOCKED`, `CUSTOMER`, `QUALITY` en `MANUAL`.

Bij een escalatie legt Cockpit huidige en return stage, niveau, reden, notitie en owner vast. Het proces gaat naar de escalation lane, escaletaken en relevante syncjobs worden aangemaakt, procesevents worden geschreven en een target kan een notificatie krijgen.

Voor oplossen is een resolution note verplicht. Zijn er nog andere escalaties open, dan blijft het proces geëscaleerd. Na de laatste oplossing keert het proces terug naar de vastgelegde return stage, met `InControlWork` als defensieve fallback, en worden de bijbehorende syncjobs gepland.

Automatische escalatie kijkt naar SLA en tijd per stap:

| Stap | Budget vóór stalled-stageescalatie |
|---|---:|
| `OrderImport` | 4 uur |
| `WorkPreparation` | 8 uur |
| `PointingInWork` | 4 uur |
| `InControlWork` | 16 uur |
| `Escalation` | 4 uur |
| `ProcessManagement` | 8 uur |

Een SLA-breach stuurt op L2, of op L3 bij minstens vier uur overschrijding. Een stalled stage stuurt op L1. Reeds geëscaleerde, terminale of ongeldige processen worden overgeslagen. De worker gebruikt `SWI_AUTO_ESCALATE_MINUTES`; er is ook een afgeschermd handmatig endpoint.

## 13.4 Externe synchronisatietaken

Bij stapovergangen gebruikt Cockpit een ingebouwd plan om jobs te maken voor onder andere AFAS, Omnitracker, Intune, Knox en Apple Business Manager. Een job bevat systeem, operatie (`READ`, `CREATE`, `UPDATE`, `DELETE`), richting, entity, reden, JSON-payload, status, pogingen, response, fout, tijdstippen en idempotency key.

```text
PENDING -> IN_PROGRESS -> DONE
                       -> FAILED
                       -> SKIPPED

FAILED, IN_PROGRESS of SKIPPED -> PENDING via retry
```

Een integratieworker met `integrations:manage` claimt pending jobs en rondt ze af. Claimen zet de status op `IN_PROGRESS` en verhoogt `attempts`. Afronden accepteert alleen `DONE`, `FAILED` of `SKIPPED`; een besliste job kan niet opnieuw worden afgerond. Retry accepteert `FAILED`, `IN_PROGRESS` en `SKIPPED`, zet de job terug op `PENDING` en wist response, fout en afrondtijd.

De idempotency key bevat order, bron- en doelstap, planindex, systeem en operatie en voorkomt dubbele geplande jobs. Een nieuwe procesimport maakt order en proces aan en plant meteen een AFAS `READ`-job voor orderregels. Bij een bestaand ordernummer geeft import de bestaande order terug en wordt alleen het proces gegarandeerd; er ontstaat geen tweede order of initiële importjob.

De repository levert de queue en het API-contract. Credentials en echte connectors voor de externe systemen zitten hier niet in.

# 14. Server-sent events

`GET /api/v1/events` vereist een aangemelde gebruiker met `orders:list`. De stream begint met een connected-comment, stuurt iedere 25 seconden een heartbeat en zet `X-Accel-Buffering: no` om proxybuffering te voorkomen.

| Event | Betekenis |
|---|---|
| `orders` | Order- of procesgerelateerde data is gewijzigd. |
| `checklist` | Picklistdata is gewijzigd. |
| `manual` | Product- of ordermanualdata is gewijzigd. |
| `comment` | Ordercomments zijn gewijzigd. |
| `automation` | Automationregels of runs zijn gewijzigd. |
| `capa` | CAPA-data is gewijzigd. |
| `notification` | De notificaties van een specifieke gebruiker zijn gewijzigd. |

Een bericht is een kleine JSON-hint met eventueel order-ID, actor en ontvanger. Het bevat geen volledige resource en heeft geen replay-ID. Iedere subscriber heeft een buffer van 16 berichten. Is die vol, dan laat de publisher een bericht voor die subscriber vallen in plaats van alle clients te blokkeren.

SSE is dus geen duurzaam of gegarandeerd eventlog. Clients moeten na een hint de leidende REST-data opnieuw ophalen. De frontend gebruikt daarnaast periodieke refresh en notificatiepolling en maakt bij onderbrekingen opnieuw verbinding.
# 15. Browserfrontend

## 15.1 Opbouw

De hoofdinterface is een single-page application in gewone JavaScript, geladen vanuit `index.html`. Navigatie loopt via hash routes zoals `#/orders/123`. De gedeelde state staat in `window.cockpitState`; functiemodules en same-origin fetchcalls doen de rest. Er is geen React- of Vue-runtime en geen npm-build nodig.

`core.js` bevat DOM-helpers, API-afhandeling, permissiechecks, formatting, escaping, dialogs, toasts, voorkeuren en gedeelde state. `app.js` coördineert navigatie en de levenscyclus van pagina’s. `auth.js` regelt wachtwoord- en QR-login, sessieherstel, refreshtimers en het starten van de applicatie.

De rol- en permissiemaps in JavaScript sturen alleen de presentatie. De Go-middleware blijft leidend voor toegang.

## 15.2 Belangrijkste JavaScriptmodules

| Bestand | Verantwoordelijkheid |
|---|---|
| `core.js` | Gedeelde state, API-helper, permissies, formatting, escaping, herbruikbare UI en voorkeuren. |
| `auth.js` | Wachtwoord- en QR-login, logout, sessieherstel, applicatie-start en refreshtimers. |
| `app.js` | Hash routing, panekeuze, navigatiebeveiliging en globale UI-coördinatie. |
| `orders.js`, `ordercreate.js`, `order-pages.js` | Orderlijsten, aanmaken, filters, bulkacties en tabs, paginering en layout van orderdetail. |
| `detail.js` | Orderdetail en interacties voor holds, QC, audit en comments. |
| `dashboard.js`, `workload.js`, `dayplan.js` | Operationele cijfers, toewijzing, workload en dagdoelen. |
| `work.js` | Klantworkflow en actieve werksessies. |
| `quality.js`, `qcbench.js` | Kwaliteitsanalyses en QC-queue, context en indiening. |
| `capa.js` | CAPA-lijst, detail, wijzigingen en verificatie. |
| `automations.js` | Beheer van automationregels en runhistorie. |
| `checklists.js` | Picklijsten en itemacties. |
| `manuals.js` | Producttemplates, ordermanuals, antwoorden en flags. |
| `si.js` | Klanten, projecten, SI-configuratie en -status en projectchecklists. |
| `users.js` | Gebruikers, afdelingen, permissies en beheer van pairingcodes. |
| `devices.js` | Prep-devices, parsed en raw detail, runhistorie en verwijderen. |
| `notifications.js` | Notificaties laden, ongelezen status en weergave. |
| `config.js` | Browserlokale import van picklijst- en configuratiedata en renderregels. |
| `scan.js`, `offline-scanner.js` | Scannerinterface, queue per gebruiker, retry en synchronisatie. |
| `pairguide.js`, `launch.js` | Ondersteuning voor native/clientpairing en launch. |
| `qrapprove.js` | QR-logingoedkeuring op de telefoon. |
| `ui.js` | Aanvullend gedeeld pagina- en UI-gedrag. |

`frontend/app.js` en `frontend/style.css` zijn oudere monolithische assets. De modulaire hoofdpagina gebruikt `frontend/js/app.js` en `frontend/styles.css`.

## 15.3 Configuratie in de browser

In Config kan JSON- en Exceldata voor picklijsten en configuratie worden geïmporteerd. Relevante instellingen staan in local storage van de browser. De hoofd-HTML laadt SheetJS en QRCode.js van cdnjs; de CSP staat die scriptorigin expliciet toe. Is het CDN niet bereikbaar, dan vallen vooral de Excel-import en -export terug. De same-origin applicatie zelf blijft bruikbaar.

Deze browserconfiguratie staat los van de omgevingsvariabelen op de server en van `app_settings` in SQLite.

## 15.4 Extra pagina’s en standalone build

- `scan.html` is de aparte scannerpagina.
- `pair-guide.html` en `launch.html` ondersteunen gepairde clients.
- `qr-approve.html` is de goedkeuringspagina voor de telefoon.
- `checkin.html` is een aanvullende statische pagina.
- `frontend/test/test.html` bevat browsertests.

`build.py` en `build.ps1` plaatsen `styles.css` en een vastgestelde subset van de JavaScriptmodules in `cockpit-standalone.html`. In die gegenereerde variant verwijderen ze ook het externe SheetJS-script. Het standalonebestand is een oudere, praktische subset en geen gegarandeerde volwaardige vervanger voor de modulaire applicatie.

# 16. Verwerking van prep-logs

`POST /api/v1/prep-logs` is bewust openbaar, zodat preparationtooling een log kan aanbieden voordat er een gebruikerssessie is. Er gelden maximaal 30 submissions per client-IP per minuut en een logpayload van 512 KiB, met een kleine marge voor de multipart-envelope.

Deze vormen worden geaccepteerd:

- `multipart/form-data` met een veld `file`;
- een raw requestbody, eventueel met een bestandsnaam in `X-File-Name`.

De parser accepteert UTF-8 en Windows-1252, normaliseert regeleindes en controleert of de inhoud op een Esprit-preplog lijkt. Daarbij kijkt hij onder andere naar voldoende regels met tijdstip, hardwaresecties en een geldige starting-hostheader. Ongeldige inhoud geeft 422 `not_a_prep_log`.

Het resultaat bevat device-identiteit, hardware en software, secties, resultaten van updatecommando’s, issues, afrondstatus en statistieken. Devices worden bijgewerkt of aangemaakt (upsert) op system UUID in hoofdletters als die beschikbaar is, anders op computernaam in hoofdletters. Bij herhaalde submissions loopt de runteller op en worden first seen en last seen bijgewerkt. Zowel de parsed JSON als de genormaliseerde raw tekst worden opgeslagen.

Voor het tonen, openen en verwijderen van prep-devices is `users:manage` vereist.

# 17. SQLite, persistentie en migraties

## 17.1 Databaseconnectie

De server gebruikt `modernc.org/sqlite`, een pure-Go SQLitedriver, via `database/sql`. De DSN zet:

- een busy timeout van vijf seconden;
- WAL journal mode;
- foreign keys aan.

De pool heeft maximaal één open en één idle connectie. Daarmee lopen databasebewerkingen binnen het proces bewust serieel, passend bij het single-writermodel van SQLite. Voor invarianten over meerdere rijen gebruiken de featurestores expliciete transacties.

## 17.2 Schemabeheer

Bij iedere start voert de server `CREATE TABLE/INDEX IF NOT EXISTS` uit en inspecteert hij bestaande kolommen met `PRAGMA table_info`. Ontbrekende legacykolommen worden met `ALTER TABLE ... ADD COLUMN` toegevoegd. Ook de index voor scanidempotentie en de tabel met barcodehistorie worden gegarandeerd.

Dit is een additieve migratiestrategie op basis van introspectie, geen framework met genummerde migraties. Bestaande databases uit oudere versies blijven zo zonder handmatig migratiecommando bruikbaar. Voor destructieve schemawijzigingen is een apart, zorgvuldig ontworpen migratiepad nodig.

## 17.3 Tabellen

| Gebied | Tabellen |
|---|---|
| Orders en kwaliteit | `orders`, `holds`, `qc_checks`, `audit_logs` |
| Identiteit en sessies | `users`, `api_tokens`, `pair_codes`, `departments`, `user_departments` |
| Samenwerking en activiteit | `comments`, `comment_mentions`, `notifications`, `scan_events`, `barcode_scans` |
| Werk en planning | `app_settings`, `day_goals`, `work_sessions`, `checklist_items`, `customer_workflows` |
| Productmanuals | `products`, `manual_blocks`, `order_manuals`, `order_manual_blocks`, `manual_tick_log` |
| SI | `customers`, `projects`, `system_integrations`, `si_project_links`, `si_events`, `project_checklist_items` |
| SWI-proces | `order_process`, `process_events`, `process_tasks`, `escalations`, `external_sync_jobs` |
| Automation en CAPA | `automation_rules`, `automation_runs`, `corrective_actions`, `capa_events` |
| Prep-logs | `prep_devices` |

Belangrijke relaties:

- historie die bij een order hoort wordt bij verwijdering op databaseniveau meestal via een cascading delete mee verwijderd;
- manualinstances verwijzen naar order en bronproduct; de gekopieerde blokken verwijzen naar de instance;
- comments hebben aparte mentionkoppelingen en notificaties verwijzen naar gebruikersnamen in plaats van user foreign keys;
- proces-, escalatie-, taak-, event- en syncjobrecords zijn direct aan orders gekoppeld;
- CAPA-events verwijzen naar corrective actions, die op hun beurt naar orders en eventueel QC-checks verwijzen;
- automationruns verwijzen naar regel en order en worden via een cascading delete verwijderd als een van die parents verdwijnt.

## 17.4 Backup en restore

`GET /api/v1/admin/backup` vereist `users:manage`. De server voert SQLite `VACUUM INTO` uit naar een tijdelijk bestand, streamt dat als `.db`-download met tijdstempel en verwijdert daarna het tijdelijke bestand. Zo ontstaat een consistente, zelfstandige database zonder handmatig kopiëren van WAL-sidecarbestanden.

Maak daarnaast apart een backup van:

- de JSON-keystore voor paired clients (`COCKPIT_CLIENT_KEYS`), met gevoelige clientsecrets;
- externe configuratie- en deploymentbestanden;
- browserlokale gegevens als die van belang zijn, want local storage staat niet in SQLite;
- pairingbestanden op clientmachines als die installaties behouden moeten blijven.

Voor een restore stopt u de server, vervangt u de database door een bekende goede backup en start u opnieuw met de juiste `DB_PATH`. Laat geen oude `-wal`- of `-shm`-sidecar van een andere databasegeneratie naast het herstelde bestand staan.

# 18. Security en deployment

## 18.1 Browser- en HTTP-beveiliging

Iedere response krijgt securityheaders, waaronder:

- een same-origin CSP met cdnjs als toegestane scriptorigin;
- `X-Content-Type-Options: nosniff`;
- `X-Frame-Options: DENY` en CSP `frame-ancestors 'none'`;
- same-origin referrer- en resourcepolicies;
- beperkingen voor camera, microfoon en geolocatie;
- HSTS als de effectieve scheme HTTPS is.

Bij `POST`, `PUT`, `PATCH` en `DELETE` moet een aanwezige `Origin`-header overeenkomen met de effectieve scheme, host en poort van het request. Samen met SameSite-cookies beschermt dit de schrijfpaden in de browser tegen cross-origin request forgery. Clients buiten de browser zonder `Origin`-header moeten nog steeds de normale authenticatie en autorisatie doorlopen.

De API geeft geen algemene CORS-toestemming voor andere origins. Zet UI en API daarom op dezelfde origin.

## 18.2 Rate limits en tijdelijke state

De limits voor login, QR, pairing, launch en prep-logs staan in het geheugen van één proces. Dat geldt ook voor SSE-subscriptions, QR-tickets, browsersessies van paired clients en de mutexes van automationworkers. Sessies, gebruikers, orders, CAPA en regels in de database zijn persistent. De definities van paired-clientsleutels staan apart in de JSON-keystore.

De beoogde deployment is daarom één applicatieproces. Meerdere replica’s delen deze geheugenstructuren niet en zouden bovendien om dezelfde SQLitedatabase concurreren. Gebruik één writer/proces, tenzij de architectuur bewust wordt aangepast.

## 18.3 Reverse proxy en TLS

Voor een HTTPS-deployment:

1. Draai de Go-server op een private, onbevoorrechte poort.
2. Termineer TLS op een vertrouwde reverse proxy.
3. Proxy statische paden, API en SSE op dezelfde publieke origin.
4. Schakel responsebuffering voor `/api/v1/events` uit en sta langdurige verbindingen toe.
5. Zet `COCKPIT_PUBLIC_URL` op de externe origin.
6. Zet `COCKPIT_TRUST_PROXY=1` alleen als de proxy forwarded headers opschoont.
7. Bewaar database en clientsleutelbestanden op persistente storage met beperkte rechten.

Secure cookies en HSTS hangen af van de effectieve HTTPS-scheme. De configuratie van het forwarded protocol moet dus kloppen.

## 18.4 Secrets en operationele gegevens

- Zet expliciet een sterk bootstrappassword of leg het eenmalig gegenereerde wachtwoord uit de eerste startlog vast.
- Wachtwoordhashes en digests van sessietokens staan in SQLite; de ruwe sessietoken gaat alleen naar de client.
- De clientkeystore bevat ruwe pairingsecrets en wordt met mode `0600` via een tijdelijk bestand en rename geschreven.
- Prep-logs kunnen hardware-ID’s, hostnames, MAC-adressen, commandoutput en IP-adressen van uploaders bevatten. Behandel ze als operationele en mogelijk persoonlijke gegevens en beperk de toegang.
- Een backupdownload bevat de volledige database en moet als gevoelig bestand worden beveiligd.

# 19. Native client en client-onlymodus

## 19.1 Pairingprotocol

Client-only is een extra UI-afscherming, geen vervanging voor de gebruikerslogin.

1. Een administrator maakt via de admin-endpoints een pairingcode of QR aan.
2. De nog niet gepairde client stuurt de code, een machine-ID en een weergavenaam naar `POST /api/v1/pair`.
3. De server consumeert de niet-verlopen, ongebruikte code atomair en maakt een client-ID en een secret van 32 bytes.
4. De secret wordt in de JSON-keystore aan die machine-ID gekoppeld. Bij een geslaagde pairing maakt de server ook een `ckdpair`-clientsessie van twaalf uur. Een pairingcode die aan een gebruiker is gekoppeld, kan bovendien de normale loginsessie van die gebruiker opzetten.
5. Bij latere launches berekent de client `HMAC-SHA256(key, timestamp + "|" + machine)` en stuurt hij de hex-proof met client-ID, timestamp en machine mee.
6. De server controleert client-ID, exacte machinekoppeling, proof en een tijdvenster van ±300 seconden. Een geldige launch maakt een nieuwe `ckdpair`-cookie van twaalf uur.

Pairing en launch zijn gelimiteerd tot tien pogingen per IP per tien minuten. Intrekken van een paired client voorkomt dat die client nog nieuwe geldige proofs kan maken.

In client-onlymodus blijven API-paden en een kleine set pairing-, approval- en healthpagina’s bereikbaar via de clientguard; hun eigen authenticatieregels blijven gelden. Andere UI- en statische paden vereisen `ckdpair` of een geldige signed launch-URL. Nadat de cookie is gezet, verwijdert een redirect de signed launchparameters.

## 19.2 Native WebView

`client/main.go` gebruikt `webview_go` en opent een venster van 1024×768 met de titel `Esprit ICT`.

- Voor de serverkeuze geldt de volgorde `COCKPIT_SERVER`, opgeslagen pairingbestand, gecompileerde standaard.
- Het pairingbestand is `cockpit-client.json` naast de executable.
- De machine-ID is SHA-256 van `COMPUTERNAME|PROCESSOR_IDENTIFIER`, vooral passend bij de beoogde Windowsomgeving.
- Wijkt de opgeslagen machine-ID af, dan verwijdert de native client het lokale pairingbestand en is opnieuw pairen vereist.
- Bij de eerste start opent de client `pair-guide.html` en bindt hij `clientMachine` en `clientPairSave` voor de pagina.
- Bij latere starts opent hij een signed root-URL met de opgeslagen sleutel.
- Het pairingbestand wordt via een tijdelijk bestand, mode `0600` en rename geschreven.

Bouw de native client vanuit zijn eigen module en directory met de platformdependencies voor WebView/GTK die `webview_go` nodig heeft:

```bash
cd client
go build -o cockpit-client .
```

Een buildomgeving voor alleen de Go-server bevat niet noodzakelijk de native ontwikkelpakketten voor WebKit en GTK.

# 20. Bouwen, testen en wijzigen

## 20.1 Serverchecks

Gebruik de toolchain uit de module; de root-`go.mod` declareert momenteel Go 1.26.4. Voer vanuit de repositoryroot uit:

```bash
gofmt -w main.go
find pkg client -type f -name '*.go' -exec gofmt -w {} +
go test ./...
go vet ./...
go mod verify
```

Zijn de native WebView-dependencies niet beschikbaar, dan kunnen de server en packages expliciet worden getest:

```bash
go test ./pkg/... .
```

Draai in een omgeving die concurrencytests aankan ook:

```bash
go test -race ./...
```

Gebruik geen dependency-tidy alleen om een onvolledige lokale modulecache te repareren. Controleer dat de oorspronkelijke `go.mod` en `go.sum` intact blijven.

## 20.2 Frontend en scripts

```bash
find frontend -type f -name '*.js' -print0 | xargs -0 -n1 node --check
python3 -m py_compile frontend/build.py
bash -n test.sh
```

Na wijzigingen in HTML of de cleanuptools moeten ook inline scripts uit HTML worden geëxtraheerd en geparsed. Genereer de standalone HTML bewust opnieuw met `python3 frontend/build.py` of `frontend/build.ps1`; behandel dat bestand niet als primaire bron.

## 20.3 Live API-regressie

Start een geïsoleerde server met een tijdelijke database en bekende bootstrapcredentials en voer daarna uit:

```bash
BASE_URL=http://localhost:8080 \
ADMIN_USERNAME=admin \
ADMIN_PASSWORD='the-test-password' \
bash test.sh
```

De livesuite test wachtwoordauthenticatie, lifecycle-, hold- en QC-gates, gebruikers, attention, scannen, comments, toewijzing, bulkacties, bescherming tegen stale writes, SSE, backup en productmanuals. De Go-tests voor domein, store en API dekken daarnaast CAPA, automations, SI, SWI, security, migraties en offline-scanidempotentie. Gebruik een wegwerpdatabase: de livesuite maakt echte records aan.

Aanvullende browsersmoketests moeten cookie- en Bearersessies, QR-goedkeuring, SSE-refresh, offline scanreplay, synchronisatie na opnieuw inloggen en client-onlypairing controleren. Voor het draaien van de native clientinterface zijn de WebView-platformdependencies nodig.

## 20.4 Veilig wijzigen

1. Bepaal eerst welk domeinpackage eigenaar is voordat u SQL of handlers wijzigt.
2. Behoud transactiegrenzen rond state en het schrijven van historie en notificaties.
3. Beoordeel routepermissies en objectregels in handlers altijd samen.
4. Pas de frontend pas aan als het servercontract vaststaat.
5. Voeg domein- en storetests toe bij gewijzigde invarianten en API-tests bij gewijzigde statussen of toegang.
6. Werk deze handleiding en de volledige routecatalogus bij als routes of gedrag veranderen.
7. Voer vóór deployment formatting, statische controles, unittests en liveregressie uit.

# 21. Volledige HTTP-routecatalogus

Deze appendix bevat alle 145 geregistreerde routes in de volgorde van de router. Methodes, paden en handlers zijn letterlijk overgenomen. De toegangskolom beschrijft de routemiddleware. `Inloggen` betekent dat een geldige sessie nodig is; een genoemde permissie wordt daarna gecontroleerd. Objectregels in handlers blijven gelden, met name voor werksessies, CAPA en geforceerd procesvoortzetten. Openbare pairing-, QR- en prep-endpoints hebben hun eigen controles op bewijs, ticket, payload en rate limit zoals eerder beschreven.

| Methode | Pad | Toegang | Handler |
|---|---|---|---|
| GET | `/healthz` | Openbaar | `handleHealth` |
| POST | `/api/v1/auth/login` | Openbaar | `handleLogin` |
| POST | `/api/v1/auth/logout` | Inloggen | `handleLogout` |
| POST | `/api/v1/pair` | Openbaar | `handleClientPair` |
| POST | `/api/v1/launch` | Openbaar | `handleClientLaunch` |
| GET | `/api/v1/qr-login` | Openbaar | `handleQRLoginCreate` |
| GET | `/api/v1/qr-login/status` | Openbaar | `handleQRLoginStatus` |
| POST | `/api/v1/qr-login/approve` | Inloggen | `handleQRLoginApprove` |
| POST | `/api/v1/prep-logs` | Openbaar | `handlePostPrepLog` |
| GET | `/api/v1/admin/prep-devices` | Inloggen + `users:manage` | `handleListPrepDevices` |
| GET | `/api/v1/admin/prep-devices/{id}` | Inloggen + `users:manage` | `handleGetPrepDevice` |
| DELETE | `/api/v1/admin/prep-devices/{id}` | Inloggen + `users:manage` | `handleDeletePrepDevice` |
| GET | `/api/v1/admin/pair-qr` | Inloggen + `users:manage` | `handleAdminPairQR` |
| POST | `/api/v1/admin/pair-code` | Inloggen + `users:manage` | `handleAdminPairCode` |
| GET | `/api/v1/admin/client-keys` | Inloggen + `users:manage` | `handleListPairedClients` |
| POST | `/api/v1/admin/client-keys/{id}/revoke` | Inloggen + `users:manage` | `handleRevokePairedClient` |
| GET | `/api/v1/auth/me` | Inloggen | `handleMe` |
| GET | `/api/v1/users` | Inloggen + `users:list` | `handleListUsers` |
| POST | `/api/v1/users` | Inloggen + `users:manage` | `handleCreateUser` |
| POST | `/api/v1/users/{id}/role` | Inloggen + `users:manage` | `handleSetUserRole` |
| POST | `/api/v1/users/{id}/password` | Inloggen + `users:manage` | `handleResetPassword` |
| DELETE | `/api/v1/users/{id}` | Inloggen + `users:manage` | `handleDeleteUser` |
| GET | `/api/v1/orders` | Inloggen + `orders:list` | `handleListOrders` |
| POST | `/api/v1/orders` | Inloggen + `orders:create` | `handleCreateOrder` |
| GET | `/api/v1/orders/{id}` | Inloggen + `orders:view` | `handleGetOrder` |
| POST | `/api/v1/orders/{id}/transition` | Inloggen + `orders:transition` | `handleTransition` |
| POST | `/api/v1/orders/{id}/assign` | Inloggen + `orders:transition` | `handleAssign` |
| POST | `/api/v1/orders/{id}/holds` | Inloggen + `holds:create` | `handlePlaceHold` |
| POST | `/api/v1/holds/{id}/resolve` | Inloggen + `holds:resolve` | `handleResolveHold` |
| POST | `/api/v1/orders/{id}/qc` | Inloggen + `qc:submit` | `handleSubmitQC` |
| GET | `/api/v1/orders/{id}/audit` | Inloggen + `audit:view` | `handleAudit` |
| POST | `/api/v1/orders/bulk-transition` | Inloggen + `orders:transition` | `handleBulkTransition` |
| POST | `/api/v1/holds/bulk-resolve` | Inloggen + `holds:resolve` | `handleBulkResolve` |
| GET | `/api/v1/orders/{id}/checklist` | Inloggen + `orders:view` | `handleGetChecklist` |
| GET | `/api/v1/stats/qc` | Inloggen + `qc:submit` | `handleQCStats` |
| GET | `/api/v1/qc/queue` | Inloggen + `qc:submit` | `handleQCQueue` |
| GET | `/api/v1/qc/reasons` | Inloggen + `qc:submit` | `handleQCReasons` |
| GET | `/api/v1/qc/context/{id}` | Inloggen + `qc:submit` | `handleQCContext` |
| GET | `/api/v1/capa` | Inloggen + `orders:list` | `handleListCAPA` |
| POST | `/api/v1/capa` | Inloggen + `qc:submit` | `handleCreateCAPA` |
| GET | `/api/v1/capa/{id}` | Inloggen + `orders:list` | `handleGetCAPA` |
| POST | `/api/v1/capa/{id}` | Inloggen | `handleUpdateCAPA` |
| POST | `/api/v1/capa/{id}/verify` | Inloggen + `qc:submit` | `handleVerifyCAPA` |
| GET | `/api/v1/automations` | Inloggen + `users:manage` | `handleListAutomationRules` |
| POST | `/api/v1/automations` | Inloggen + `users:manage` | `handleCreateAutomationRule` |
| POST | `/api/v1/automations/{id}` | Inloggen + `users:manage` | `handleUpdateAutomationRule` |
| DELETE | `/api/v1/automations/{id}` | Inloggen + `users:manage` | `handleDeleteAutomationRule` |
| GET | `/api/v1/automation-runs` | Inloggen + `users:manage` | `handleListAutomationRuns` |
| POST | `/api/v1/automations/evaluate` | Inloggen + `users:manage` | `handleEvaluateAutomations` |
| GET | `/api/v1/dayplan` | Inloggen + `orders:list` | `handleGetDayplan` |
| PUT | `/api/v1/dayplan/goal` | Inloggen + `qc:submit` | `handlePutDayGoal` |
| POST | `/api/v1/dayplan/send` | Inloggen + `qc:submit` | `handleSendDayGoal` |
| GET | `/api/v1/stats/ops` | Inloggen + `orders:list` | `handleOpsStats` |
| GET | `/api/v1/workflow` | Inloggen + `orders:list` | `handleGetWorkflow` |
| PUT | `/api/v1/workflow` | Inloggen + `users:manage` | `handlePutWorkflow` |
| GET | `/api/v1/work/sessions` | Inloggen + `users:manage` | `handleListAllWork` |
| GET | `/api/v1/work/mine` | Inloggen + `orders:view` | `handleListMyWork` |
| GET | `/api/v1/orders/{id}/work` | Inloggen + `orders:view` | `handleGetWork` |
| POST | `/api/v1/orders/{id}/work/start` | Inloggen + `orders:view` | `handleStartWork` |
| POST | `/api/v1/orders/{id}/work/update` | Inloggen + `orders:view` | `handleUpdateWork` |
| POST | `/api/v1/orders/{id}/work/end` | Inloggen + `orders:view` | `handleEndWork` |
| POST | `/api/v1/orders/{id}/checklist` | Inloggen + `pick:use` | `handleSetChecklist` |
| POST | `/api/v1/checklist/{id}/tick` | Inloggen + `pick:use` | `handleTickChecklistItem` |
| POST | `/api/v1/checklist/{id}/untick` | Inloggen + `pick:use` | `handleUntickChecklistItem` |
| GET | `/api/v1/checklists` | Inloggen + `orders:view` | `handleListChecklists` |
| POST | `/api/v1/checklists/demo` | Inloggen + `orders:create` | `handleSeedDemoChecklists` |
| GET | `/api/v1/products` | Inloggen + `orders:list` | `handleListProducts` |
| POST | `/api/v1/products` | Inloggen + `manuals:manage` | `handleCreateProduct` |
| GET | `/api/v1/products/{id}` | Inloggen + `orders:view` | `handleGetProduct` |
| POST | `/api/v1/products/{id}` | Inloggen + `manuals:manage` | `handleUpdateProduct` |
| DELETE | `/api/v1/products/{id}` | Inloggen + `manuals:manage` | `handleDeleteProduct` |
| POST | `/api/v1/products/quick` | Inloggen + `manuals:manage` | `handleQuickManual` |
| POST | `/api/v1/products/{id}/approve` | Inloggen + `manuals:approve` | `handleApproveProduct` |
| POST | `/api/v1/products/{id}/department` | Inloggen + `manuals:manage` | `handleSetProductDepartment` |
| POST | `/api/v1/si/customers/{id}/coordinator` | Inloggen + `customers:manage` | `handleSetCustomerCoordinator` |
| GET | `/api/v1/customers` | Inloggen + `orders:list` | `handleListCustomersDirectory` |
| POST | `/api/v1/customers` | Inloggen + `customers:manage` | `handleCreateCustomerManaged` |
| GET | `/api/v1/customers/flows` | Inloggen + `orders:list` | `handleListCustomerFlows` |
| POST | `/api/v1/products/{id}/blocks` | Inloggen + `manuals:manage` | `handleAddManualBlock` |
| POST | `/api/v1/products/{pid}/blocks/{bid}` | Inloggen + `manuals:manage` | `handleUpdateManualBlock` |
| POST | `/api/v1/products/{pid}/blocks/{bid}/move` | Inloggen + `manuals:manage` | `handleMoveManualBlock` |
| DELETE | `/api/v1/products/{pid}/blocks/{bid}` | Inloggen + `manuals:manage` | `handleDeleteManualBlock` |
| POST | `/api/v1/orders/{id}/manuals` | Inloggen + `pick:use` | `handleInstantiateManual` |
| GET | `/api/v1/orders/{id}/manuals` | Inloggen + `orders:view` | `handleListOrderManuals` |
| POST | `/api/v1/manuals/{mid}/blocks/{bid}/answer` | Inloggen + `pick:use` | `handleAnswerManualBlock` |
| POST | `/api/v1/manuals/{mid}/blocks/{bid}/flag` | Inloggen + `manuals:manage` | `handleFlagManualBlock` |
| DELETE | `/api/v1/manuals/{mid}` | Inloggen + `manuals:manage` | `handleDeleteManual` |
| GET | `/api/v1/manuals` | Inloggen + `orders:view` | `handleListManuals` |
| GET | `/api/v1/attention` | Inloggen + `orders:list` | `handleAttention` |
| POST | `/api/v1/scan` | Inloggen + `scan:use` | `handleScan` |
| GET | `/api/v1/orders/{id}/scans` | Inloggen + `orders:list` | `handleListScans` |
| GET | `/api/v1/orders/{id}/comments` | Inloggen + `comments:read` | `handleListComments` |
| POST | `/api/v1/orders/{id}/comments` | Inloggen + `comments:post` | `handleAddComment` |
| GET | `/api/v1/orders/{id}/barcode` | Inloggen + `orders:view` | `handleGetBarcode` |
| POST | `/api/v1/orders/{id}/barcode` | Inloggen + `orders:transition` | `handleGenerateBarcode` |
| POST | `/api/v1/orders/{id}/omnitracker` | Inloggen + `orders:transition` | `handleUpdateOmnitrackerInfo` |
| GET | `/api/v1/orders/{id}/barcode/history` | Inloggen + `orders:view` | `handleBarcodeScanHistory` |
| GET | `/api/v1/scan/barcode` | Inloggen + `scan:use` | `handleScanBarcode` |
| POST | `/api/v1/process/orders/import` | Inloggen + `process:advance` | `handleImportOrder` |
| GET | `/api/v1/process/board` | Inloggen + `process:view` | `handleProcessBoard` |
| GET | `/api/v1/process/metrics` | Inloggen + `process:view` | `handleProcessMetrics` |
| GET | `/api/v1/process/escalations` | Inloggen + `process:view` | `handleListEscalations` |
| POST | `/api/v1/process/escalations/auto` | Inloggen + `process:manage` | `handleAutoEscalate` |
| POST | `/api/v1/process/escalations/{id}/resolve` | Inloggen + `process:escalate` | `handleResolveEscalation` |
| GET | `/api/v1/process/sync-jobs` | Inloggen + `process:view` | `handleListSyncJobs` |
| POST | `/api/v1/process/sync-jobs/claim` | Inloggen + `integrations:manage` | `handleClaimSyncJobs` |
| POST | `/api/v1/process/sync-jobs/{id}/complete` | Inloggen + `integrations:manage` | `handleCompleteSyncJob` |
| POST | `/api/v1/process/sync-jobs/{id}/retry` | Inloggen + `integrations:manage` | `handleRetrySyncJob` |
| GET | `/api/v1/orders/{id}/process` | Inloggen + `process:view` | `handleGetProcess` |
| POST | `/api/v1/orders/{id}/process/advance` | Inloggen + `process:advance` | `handleAdvanceProcess` |
| POST | `/api/v1/orders/{id}/process/escalate` | Inloggen + `process:escalate` | `handleEscalateProcess` |
| GET | `/api/v1/orders/{id}/process/tasks` | Inloggen + `process:view` | `handleProcessTasks` |
| POST | `/api/v1/process/tasks/{id}/tick` | Inloggen + `process:advance` | `handleTickProcessTask` |
| POST | `/api/v1/process/tasks/{id}/untick` | Inloggen + `process:advance` | `handleUntickProcessTask` |
| GET | `/api/v1/orders/{id}/process/events` | Inloggen + `process:view` | `handleProcessEvents` |
| GET | `/api/v1/notifications` | Inloggen | `handleListNotifications` |
| POST | `/api/v1/notifications/read` | Inloggen | `handleMarkNotificationsRead` |
| GET | `/api/v1/users/brief` | Inloggen + `orders:list` | `handleListUsersBrief` |
| GET | `/api/v1/si/customers` | Inloggen + `si:list` | `handleListCustomers` |
| POST | `/api/v1/si/customers` | Inloggen + `si:create` | `handleCreateCustomer` |
| GET | `/api/v1/si/customers/{number}/projects` | Inloggen + `si:list` | `handleListProjects` |
| POST | `/api/v1/si/projects` | Inloggen + `si:create` | `handleCreateProject` |
| GET | `/api/v1/si/sis` | Inloggen + `si:list` | `handleListSIs` |
| POST | `/api/v1/si/sis` | Inloggen + `si:create` | `handleCreateSI` |
| GET | `/api/v1/si/sis/{id}` | Inloggen + `si:view` | `handleGetSI` |
| POST | `/api/v1/si/sis/{id}` | Inloggen + `si:update` | `handleUpdateSI` |
| POST | `/api/v1/si/sis/{id}/transition` | Inloggen + `si:transition` | `handleTransitionSI` |
| POST | `/api/v1/si/sis/{id}/projects` | Inloggen + `si:projects` | `handleSetSIProjects` |
| GET | `/api/v1/si/sis/{id}/events` | Inloggen + `si:view` | `handleSIEvents` |
| POST | `/api/v1/si/seed` | Inloggen + `si:create` | `handleSeedDemoSI` |
| GET | `/api/v1/si/projects/{projectId}/checklist` | Inloggen + `si:view` | `handleListChecklist` |
| POST | `/api/v1/si/projects/{projectId}/checklist` | Inloggen + `si:update` | `handleAddChecklistItem` |
| POST | `/api/v1/si/projects/checklist/{itemId}/tick` | Inloggen + `si:update` | `handleSITickChecklistItem` |
| POST | `/api/v1/si/projects/checklist/{itemId}/untick` | Inloggen + `si:update` | `handleSIUntickChecklistItem` |
| DELETE | `/api/v1/si/projects/checklist/{itemId}` | Inloggen + `si:update` | `handleDeleteChecklistItem` |
| GET | `/api/v1/departments` | Inloggen + `users:list` | `handleListDepartments` |
| POST | `/api/v1/departments` | Inloggen + `users:manage` | `handleCreateDepartment` |
| DELETE | `/api/v1/departments/{id}` | Inloggen + `users:manage` | `handleDeleteDepartment` |
| POST | `/api/v1/departments/{id}/users` | Inloggen + `users:manage` | `handleAddUserToDepartment` |
| DELETE | `/api/v1/departments/{id}/users/{userId}` | Inloggen + `users:manage` | `handleRemoveUserFromDepartment` |
| GET | `/api/v1/departments/{id}/users` | Inloggen + `users:manage` | `handleListDepartmentUsers` |
| POST | `/api/v1/departments/{id}/permissions` | Inloggen + `users:manage` | `handleSetDepartmentPermissions` |
| GET | `/api/v1/users/{id}/departments` | Inloggen + `users:list` | `handleListUserDepartments` |
| GET | `/api/v1/admin/backup` | Inloggen + `users:manage` | `handleAdminBackup` |
| GET | `/api/v1/events` | Inloggen + `orders:list` | `handleEvents` |
