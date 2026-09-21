package swi

import "time"

type TaskTemplate struct {
	Seq         int    `json:"seq"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Role        string `json:"role"`
	Required    bool   `json:"required"`
}

type Task struct {
	ID          int64      `json:"id"`
	OrderID     int64      `json:"orderId"`
	Stage       Stage      `json:"stage"`
	Seq         int        `json:"seq"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Role        string     `json:"role,omitempty"`
	Required    bool       `json:"required"`
	Done        bool       `json:"done"`
	DoneBy      string     `json:"doneBy,omitempty"`
	DoneAt      *time.Time `json:"doneAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
}

var stageTemplates = map[Stage][]TaskTemplate{
	StageOrderImport: {
		{Seq: 1, Title: "AFAS-ordergegevens controleren", Description: "Debiteurnummer, klantnaam, device en assetnummer compleet en correct.", Role: "operator", Required: true},
		{Seq: 2, Title: "Omnitracker-ticket koppelen", Description: "Ticketnummer vastleggen zodat het werk traceerbaar is.", Role: "operator", Required: true},
		{Seq: 3, Title: "Systeemintegratie (SI) bepalen", Description: "SI koppelen of vastleggen dat de order zonder SI loopt.", Role: "operator"},
	},
	StageWorkPreparation: {
		{Seq: 1, Title: "Picklijst gereedmaken", Description: "Onderdelen en aantallen controleren op de picklijst.", Role: "operator", Required: true},
		{Seq: 2, Title: "Productmanual koppelen", Description: "Het juiste manual aan de order hangen zodat de stappen bekend zijn.", Role: "operator", Required: true},
		{Seq: 3, Title: "Onderdelen en materialen controleren", Description: "Voorraad controleren; ontbrekende delen melden.", Role: "operator"},
		{Seq: 4, Title: "Barcode genereren en printen", Description: "Barcode/QR genereren en aan de fysieke werkorder hangen.", Role: "operator"},
	},
	StagePointingInWork: {
		{Seq: 1, Title: "Engineer toewijzen", Description: "Werk toewijzen aan de juiste engineer/team (assignee).", Role: "operator", Required: true},
		{Seq: 2, Title: "Device aanmelden in Intune", Description: "Device inschrijven en configuratieprofiel toewijzen.", Role: "operator", Required: true},
		{Seq: 3, Title: "Device aanmelden in Knox / ABM", Description: "Samsung Knox-profiel of Apple Business Manager-toewijzing vastleggen.", Role: "operator"},
		{Seq: 4, Title: "Werkplek en planning afstemmen", Description: "Afspraak met klant/engineer vastleggen.", Role: "operator"},
	},
	StageInControlWork: {
		{Seq: 1, Title: "Uitvoering afronden", Description: "Werkzaamheden conform manual afgerond.", Role: "operator", Required: true},
		{Seq: 2, Title: "Inwerkcontrole (4-ogen)", Description: "Tweede medewerker controleert het uitgevoerde werk.", Role: "operator", Required: true},
		{Seq: 3, Title: "QC-check uitvoeren", Description: "QC PASS/FAIL vastleggen.", Role: "qc", Required: true},
		{Seq: 4, Title: "Afwijkingen registreren", Description: "Afwijkingen of herwerk vastleggen op de order.", Role: "qc"},
	},
	StageEscalation: {
		{Seq: 1, Title: "Escalatie beoordelen", Description: "Vaststellen wat de oorzaak en impact is.", Role: "operator", Required: true},
		{Seq: 2, Title: "Eigenaar en actie bepalen", Description: "Wie pakt het op en welke actie volgt.", Role: "process_owner", Required: true},
		{Seq: 3, Title: "Besluit terugkoppelen", Description: "Klant/engineer informeren over het besluit.", Role: "process_owner"},
		{Seq: 4, Title: "Escalatie afsluiten", Description: "Escalatie afsluiten en proces terugzetten in de flow.", Role: "process_owner", Required: true},
	},
	StageProcessManagement: {
		{Seq: 1, Title: "Doorlooptijd (TAT) controleren", Description: "Controleren of de order binnen de afgesproken doorlooptijd is gebleven.", Role: "process_owner", Required: true},
		{Seq: 2, Title: "Oorzaak en verbeteractie vastleggen", Description: "Root cause bij afwijkingen en de verbeteractie noteren.", Role: "process_owner"},
		{Seq: 3, Title: "Bronsystemen bijwerken", Description: "AFAS, Omnitracker en MDM-systemen gelijk trekken met de werkelijke situatie.", Role: "integrations", Required: true},
		{Seq: 4, Title: "Order afsluiten", Description: "Order afronden en de processtap Completed zetten.", Role: "process_owner", Required: true},
	},
}

func StageTasks(s Stage) []TaskTemplate {
	out := stageTemplates[s]
	if len(out) == 0 {
		return nil
	}
	cp := make([]TaskTemplate, len(out))
	copy(cp, out)
	return cp
}

func TaskProgress(tasks []Task) (total, done int, requiredOpen int) {
	for _, t := range tasks {
		total++
		if t.Done {
			done++
			continue
		}
		if t.Required {
			requiredOpen++
		}
	}
	return total, done, requiredOpen
}
