package app

import (
	"strings"

	"github.com/albievan/cactusinv/internal/domain"
)

// signedIn marks an operation every signed-in user may run: their own account, and the
// reference data every page reads (currencies, the business profile, bank accounts —
// all printed on every invoice anyway).
var signedIn = []domain.Permission{}

// operationAccess maps each API operation (its OpenAPI operationId, lower case) to the
// permissions that allow it — any one of them is enough. An operation missing from the
// map is refused for every session: a new endpoint must be given its permission here
// (the HTTP tests check every operation in the contract is listed) (ADR-012).
var operationAccess = map[string][]domain.Permission{
	"login": signedIn, "getversion": signedIn, "logout": signedIn, "changepassword": signedIn, "me": signedIn,
	"listcurrencies": signedIn, "getbusiness": signedIn, "listbankaccounts": signedIn, "getbankaccount": signedIn,

	"getdashboard": {domain.PermDashboardView},

	"listinvoices": {domain.PermInvoicesView}, "getinvoice": {domain.PermInvoicesView}, "getinvoicepdf": {domain.PermInvoicesView},
	"createinvoice": {domain.PermInvoicesCreate}, "updateinvoice": {domain.PermInvoicesEdit},
	"deleteinvoice": {domain.PermInvoicesDelete}, "issueinvoice": {domain.PermInvoicesIssue},
	"sendinvoice": {domain.PermInvoicesSend},
	"voidinvoice": {domain.PermInvoicesVoid}, "markinvoiceoverdue": {domain.PermInvoicesOverdue}, "markinvoicepaid": {domain.PermInvoicesPay},

	"listcustomers": {domain.PermCustomersView}, "getcustomer": {domain.PermCustomersView},
	"createcustomer": {domain.PermCustomersCreate}, "updatecustomer": {domain.PermCustomersEdit}, "deletecustomer": {domain.PermCustomersDelete},

	"liststatements": {domain.PermStatementsView}, "getstatement": {domain.PermStatementsView}, "getstatementpdf": {domain.PermStatementsView},
	"sendstatement": {domain.PermStatementsSend}, "sendstatements": {domain.PermStatementsSend},

	"listproducts": {domain.PermProductsView}, "getproduct": {domain.PermProductsView},
	"createproduct": {domain.PermProductsCreate}, "updateproduct": {domain.PermProductsEdit}, "deleteproduct": {domain.PermProductsDelete},

	"listschedules": {domain.PermSchedulesView}, "getschedule": {domain.PermSchedulesView},
	"previewschedule": {domain.PermSchedulesView}, "getscheduledefaults": {domain.PermSchedulesView},
	"createschedule": {domain.PermSchedulesCreate}, "updateschedule": {domain.PermSchedulesEdit}, "deleteschedule": {domain.PermSchedulesDelete},
	"pauseschedule": {domain.PermSchedulesPause}, "resumeschedule": {domain.PermSchedulesResume}, "runschedulenow": {domain.PermSchedulesRun},

	// A statement page lists the statements e-mailed to its customer.
	"listemails":  {domain.PermEmailsView, domain.PermStatementsView},
	"cancelemail": {domain.PermEmailsCancel}, "retryemail": {domain.PermEmailsRetry},

	"listaudit": {domain.PermAuditView}, "verifyaudit": {domain.PermAuditVerify},

	"getemailsettings":    {domain.PermSettingsEmailView},
	"updatebusiness":      {domain.PermSettingsBusiness},
	"updateemailsettings": {domain.PermSettingsEmail}, "sendtestemail": {domain.PermSettingsEmailTest},
	"createcurrency": {domain.PermSettingsCurrenciesCreate}, "updatecurrency": {domain.PermSettingsCurrenciesEdit},
	"createbankaccount": {domain.PermSettingsBanksCreate}, "updatebankaccount": {domain.PermSettingsBanksEdit}, "deletebankaccount": {domain.PermSettingsBanksDelete},

	"listbackups": {domain.PermAdminister}, "createbackup": {domain.PermAdminister}, "exportbackup": {domain.PermAdminister},
	"uploadbackup": {domain.PermAdminister}, "stagerestore": {domain.PermAdminister}, "getcurrentrestore": {domain.PermAdminister},
	"applyrestore": {domain.PermAdminister}, "discardrestore": {domain.PermAdminister}, "resumeautomation": {domain.PermAdminister},
	"listusers": {domain.PermAdminister}, "adduser": {domain.PermAdminister}, "resetuserpassword": {domain.PermAdminister},
	"disableuser": {domain.PermAdminister}, "enableuser": {domain.PermAdminister}, "setusergroups": {domain.PermAdminister},
	"listgroups": {domain.PermAdminister}, "creategroup": {domain.PermAdminister}, "updategroup": {domain.PermAdminister},
	"deletegroup": {domain.PermAdminister},
	// A settings file holds every customer's contact details and the bank account numbers,
	// and an import changes many things at once: each has its own permission (M24).
	"exportsettings": {domain.PermSettingsExport}, "importsettings": {domain.PermSettingsImport},
	"getsystemsettings": {domain.PermSettingsSystemView}, "updatesystemsettings": {domain.PermSettingsSystemEdit},
}

// OperationKnown reports whether an operation has an access rule (for the contract test).
func OperationKnown(operationID string) bool {
	_, ok := operationAccess[strings.ToLower(operationID)]
	return ok
}

// SessionAllows decides whether a signed-in user's session may run an API operation,
// named by its OpenAPI operationId (compared ignoring letter case). While the user must
// change their password, only reading their own account (me), changing the password
// (changePassword) and signing out (logout) are allowed: ErrPasswordChangeRequired
// (ADR-011). Otherwise the user's access must include one of the operation's permissions,
// and an operation without a rule is refused: ErrNoAccess (ADR-012).
func SessionAllows(u domain.User, operationID string) error {
	op := strings.ToLower(operationID)
	if u.MustChangePassword {
		switch op {
		case "me", "changepassword", "logout":
			return nil
		}
		return domain.ErrPasswordChangeRequired
	}
	perms, ok := operationAccess[op]
	if !ok {
		return domain.ErrNoAccess
	}
	if len(perms) == 0 {
		return nil
	}
	for _, p := range perms {
		if u.Access.Has(p) {
			return nil
		}
	}
	return domain.ErrNoAccess
}
