package services

import (
	"fmt"
	"strconv"
	"time"

	"github.com/anxi0uz/logiflow/internal/events"
	"github.com/anxi0uz/logiflow/internal/models"
)

func transportDocumentFields(order *models.Order, receipt *models.DeliveryReceipt) ([]events.DocumentField, error) {
	fields := []events.DocumentField{}
	details, err := readTransportDetails(order)
	if err != nil {
		return nil, err
	}
	if details != nil {
		for _, party := range []struct {
			label  string
			record models.Counterparty
		}{{"Грузоотправитель", details.Sender}, {"Грузополучатель", details.Recipient}, {"Перевозчик", details.Carrier}} {
			p := party.record
			fields = append(fields, events.DocumentField{Label: party.label, Value: fmt.Sprintf("%s\nИНН: %s; КПП: %s\nАдрес: %s\nКонтакт: %s; телефон: %s; email: %s", p.Name, p.INN, p.KPP, p.Address, p.ContactName, p.Phone, p.Email)})
		}
		basis := "Договор"
		if details.BasisType == "transport_request" {
			basis = "Заявка на перевозку"
		}
		fields = append(fields, events.DocumentField{Label: "Основание перевозки", Value: fmt.Sprintf("%s № %s от %s", basis, details.BasisNumber, details.BasisDate)}, events.DocumentField{Label: "Заявлено грузовых мест", Value: strconv.Itoa(details.PackageCount)})
		if details.Terms != "" {
			fields = append(fields, events.DocumentField{Label: "Условия перевозки", Value: details.Terms})
		}
	}
	if receipt != nil {
		fields = append(fields,
			events.DocumentField{Label: "Фактически принял груз", Value: receipt.RecipientName + " / " + receipt.RecipientRole},
			events.DocumentField{Label: "Основание приёмки (со слов подтверждающего)", Value: receipt.AuthorityBasis},
			events.DocumentField{Label: "Фактически принято мест", Value: strconv.Itoa(receipt.PackageCount)},
			events.DocumentField{Label: "Состояние при приёмке", Value: receipt.CargoCondition},
			events.DocumentField{Label: "Пломба при приёмке", Value: receipt.SealNumber},
			events.DocumentField{Label: "Замечания при приёмке", Value: receipt.Comment},
			events.DocumentField{Label: "Время фактической приёмки", Value: receipt.ReceivedAt.UTC().Format(time.RFC3339)},
			events.DocumentField{Label: "Запись приёмки", Value: fmt.Sprintf("%s; назначение %s; зафиксировал %s; время записи %s", receipt.ID, receipt.AssignmentID, receipt.RecordedByUserID, receipt.RecordedAt.UTC().Format(time.RFC3339))})
	}
	return fields, nil
}
