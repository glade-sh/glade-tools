# Salesforce Coverage Manifest

- Source documents: 3224
- Source members: 5172
- Coverage entries: 8396
- Known supported entries: 260
- Unknown entries: 6997
- Tooling API classes: 7091
- Tooling API members: 73326
- Runtime APIs found in Tooling API: 195/245

| Area | Target | Entries | Supported | Partial | Stub | Unsupported | Unknown |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Core stdlib | `executable-parity` | 968 | 108 | 28 | 0 | 0 | 832 |
| Data platform | `local-model` | 835 | 128 | 7 | 0 | 0 | 700 |
| Integration, security, and UI | `local-model` | 758 | 19 | 7 | 0 | 0 | 732 |
| Language and guide docs | `unsupported` | 1092 | 2 | 0 | 0 | 1090 | 0 |
| Product namespaces | `typed-stub` | 4615 | 0 | 0 | 0 | 0 | 4615 |
| Tests, async, and limits | `local-model` | 128 | 3 | 7 | 0 | 0 | 118 |

## Tooling API System Alignment

Source: `tooling_system_symbols.json.gz`

- Namespaces: 145
- Classes: 7091
- Constructors: 5807
- Methods: 40522
- Properties: 26997
- System-default namespace classes: 198
- System-default namespace members: 3280
- Concrete runtime APIs in Tooling API: 195/245
- Catalog system entries in Tooling API: 1985/2689

### Runtime APIs Not Found In Tooling API
- `AccessLevel.withPermissionSetId(String)`
- `Answers.findSimilar(Question)`
- `ApexPages.addMessages(Exception)`
- `ApexPages.addMessages(Object)`
- `Approval.process(Approval.ProcessRequest)`
- `Boolean.valueOf(String)`
- `Database.lock`
- `Database.unlock`
- `Decimal.divide(Decimal,Integer,RoundingMode)`
- `Http.send(HttpRequest)`
- `Label.get(String,String)`
- `Label.get(String,String,String)`
- `Label.translationExists(String,String,String)`
- `Limits.getAsyncCalls`
- `Limits.getLimitAsyncCalls`
- `Messaging.renderStoredEmailTemplate(String,String,String,Messaging.AttachmentRetrievalOption)`
- `Messaging.renderStoredEmailTemplate(String,String,String,Messaging.AttachmentRetrievalOption,Boolean)`
- `Messaging.sendEmail(Messaging.Email[],Boolean)`
- `QuickAction.describeAvailableActions`
- `SObject.setOptions(Database.DMLOptions)`
- `SandboxPostCopy.runApexClass(SandboxContext)`
- `Schedulable.execute(SchedulableContext)`
- `Search.find(String,AccessLevel)`
- `Search.find(String,Object)`
- `Search.query(String,AccessLevel)`

### Catalog System Entries Not Found In Tooling API
- `AccessLevel.SYSTEM\_MODE`
- `AccessLevel.USER\_MODE`
- `AccessLevel.withPermissionSetId`
- `ApexPages.ApexPages`
- `ApexPages.KnowledgeArticleVersionStandardController.setDataCategory`
- `ApexPages.StandardController.reset`
- `Approval.Approval`
- `Approval.process`
- `Approval.process`
- `Approval.process`
- `Approval.process`
- `Auth.Auth`
- `Auth.AuthConfiguration`
- `Auth.AuthConfiguration.AuthConfiguration`
- `Auth.AuthConfiguration.getAllowInternalUserLoginEnabled`
- `Auth.AuthConfiguration.getAuthConfig`
- `Auth.AuthConfiguration.getAuthConfigProviders`
- `Auth.AuthConfiguration.getAuthProviders`
- `Auth.AuthConfiguration.getAuthProviderSsoDomainUrl`
- `Auth.AuthConfiguration.getAuthProviderSsoUrl`
- `Auth.AuthConfiguration.getBackgroundColor`
- `Auth.AuthConfiguration.getCertificateLoginEnabled`
- `Auth.AuthConfiguration.getCertificateLoginUrl`
- `Auth.AuthConfiguration.getDefaultProfileForRegistration`
- `Auth.AuthConfiguration.getFooterText`
