// SF-CB193 local deterministic mock fixture. Every selected row has a direct
// assertion; no delivery, hosted callback, scheduling, or capacity behavior is
// exercised here.

// Cache.Org: contains(String), contains(List<String>), contains(Set<String>).
Cache.Org.put('cb193-org-basic', 'basic');
System.assertEquals(true, Cache.Org.contains('cb193-org-basic'));
List<Boolean> orgListContains = Cache.Org.contains(new List<String>{'cb193-org-basic', 'cb193-org-missing'});
System.assertEquals(true, orgListContains[0]);
System.assertEquals(false, orgListContains[1]);
Map<String, Boolean> orgSetContains = Cache.Org.contains(new Set<String>{'cb193-org-basic', 'cb193-org-missing'});
System.assertEquals(true, orgSetContains.get('cb193-org-basic'));
System.assertEquals(false, orgSetContains.get('cb193-org-missing'));

// Cache.Org: put(String,Object), put(String,Object,Visibility),
// put(String,Object,Integer), put(String,Object,Integer,Visibility,Boolean).
Cache.Org.put('cb193-org-basic-put', 'basic-put');
System.assertEquals('basic-put', (String) Cache.Org.get('cb193-org-basic-put'));
Cache.Org.put('cb193-org-visible-put', 'visible-put', Cache.Visibility.ALL);
System.assertEquals('visible-put', (String) Cache.Org.get('cb193-org-visible-put'));
Cache.Org.put('cb193-org-ttl-put', 'ttl-put', 300);
System.assertEquals('ttl-put', (String) Cache.Org.get('cb193-org-ttl-put'));
Cache.Org.put('cb193-org-full-put', 'full-put', 300, Cache.Visibility.ALL, false);
System.assertEquals('full-put', (String) Cache.Org.get('cb193-org-full-put'));

// Cache.Org: remove(List<String>), remove(String).
Cache.Org.put('cb193-org-list-remove-a', 'list-a');
Cache.Org.put('cb193-org-list-remove-b', 'list-b');
List<Boolean> orgRemoved = Cache.Org.remove(new List<String>{'cb193-org-list-remove-a', 'cb193-org-list-remove-b'});
System.assertEquals(true, orgRemoved[0]);
System.assertEquals(true, orgRemoved[1]);
Cache.Org.put('cb193-org-string-remove', 'string-remove');
Boolean orgStringRemoved = Cache.Org.remove('cb193-org-string-remove');
System.assertEquals(true, orgStringRemoved);

// Cache.Session: contains(String), contains(Set<String>).
Cache.Session.put('cb193-session-basic', 'session-basic');
System.assertEquals(true, Cache.Session.contains('cb193-session-basic'));
Map<String, Boolean> sessionSetContains = Cache.Session.contains(new Set<String>{'cb193-session-basic', 'cb193-session-missing'});
System.assertEquals(true, sessionSetContains.get('cb193-session-basic'));
System.assertEquals(false, sessionSetContains.get('cb193-session-missing'));

// Cache.Session: put(String,Object), put(String,Object,Visibility),
// put(String,Object,Integer), put(String,Object,Integer,Visibility,Boolean).
Cache.Session.put('cb193-session-basic-put', 'basic-put');
System.assertEquals('basic-put', (String) Cache.Session.get('cb193-session-basic-put'));
Cache.Session.put('cb193-session-visible-put', 'visible-put', Cache.Visibility.ALL);
System.assertEquals('visible-put', (String) Cache.Session.get('cb193-session-visible-put'));
Cache.Session.put('cb193-session-ttl-put', 'ttl-put', 300);
System.assertEquals('ttl-put', (String) Cache.Session.get('cb193-session-ttl-put'));
Cache.Session.put('cb193-session-full-put', 'full-put', 300, Cache.Visibility.ALL, false);
System.assertEquals('full-put', (String) Cache.Session.get('cb193-session-full-put'));

// Cache.Session: remove(String).
Cache.Session.put('cb193-session-string-remove', 'string-remove');
Boolean sessionStringRemoved = Cache.Session.remove('cb193-session-string-remove');
System.assertEquals(true, sessionStringRemoved);

// Metadata: CustomMetadata, CustomMetadataValue, DeployContainer,
// DeployResult, DeployStatus, null-callback enqueue, four status overloads,
// and the two local CustomMetadata retrieve overloads.
Metadata.CustomMetadata item = new Metadata.CustomMetadata();
item.fullName = 'Feature.CB193';
item.label = 'CB193';
Metadata.CustomMetadataValue value = new Metadata.CustomMetadataValue();
value.field = 'Enabled__c';
value.value = true;
item.values.add(value);
System.assertEquals('Feature.CB193', item.fullName);
System.assertEquals('Enabled__c', item.values[0].field);
System.assertEquals(true, (Boolean) item.values[0].value);
Metadata.DeployContainer container = new Metadata.DeployContainer();
container.addMetadata(item);
System.assertEquals(1, container.getMetadata().size());
Id deploymentId = Metadata.Operations.enqueueDeployment(container, null);
Metadata.DeployResult statusById = Metadata.Operations.checkDeployStatus(deploymentId);
Metadata.DeployResult statusByIdWithDetails = Metadata.Operations.checkDeployStatus(deploymentId, true);
String deploymentText = (String) deploymentId;
Metadata.DeployResult statusByString = Metadata.Operations.checkDeployStatus(deploymentText);
Metadata.DeployResult statusByStringWithoutDetails = Metadata.Operations.checkDeployStatus(deploymentText, false);
System.assert(statusById != null);
System.assert(statusByIdWithDetails != null);
System.assert(statusByString != null);
System.assert(statusByStringWithoutDetails != null);
System.assert(statusById.success);
System.assertEquals(Metadata.DeployStatus.Succeeded, statusById.status);
System.assertEquals(Metadata.DeployStatus.Succeeded, statusByIdWithDetails.status);
System.assertEquals(Metadata.DeployStatus.Succeeded, statusByString.status);
System.assertEquals(Metadata.DeployStatus.Succeeded, statusByStringWithoutDetails.status);
List<Metadata.CustomMetadata> rows = Metadata.Operations.retrieve(Metadata.MetadataType.CustomMetadata, new List<String>{'Feature.CB193'});
List<Metadata.CustomMetadata> rowsWithProtected = Metadata.Operations.retrieve(Metadata.MetadataType.CustomMetadata, new List<String>{'Feature.CB193'}, true);
System.assertEquals(1, rows.size());
System.assertEquals(1, rowsWithProtected.size());
System.assertEquals('Feature.CB193', rows[0].fullName);
System.assertEquals('Enabled__c', rows[0].values[0].field);
System.assertEquals(true, (Boolean) rows[0].values[0].value);
System.assertEquals('Feature.CB193', rowsWithProtected[0].fullName);

// Messaging.Email constructor plus seven direct getter/setter pairs.
Messaging.Email email = new Messaging.Email();
email.setBccSender(true);
email.setEmailPriority('High');
email.setReplyTo('reply@cb193.example');
email.setSaveAsActivity(false);
email.setSenderDisplayName('CB193 Sender');
email.setSubject('CB193 Subject');
email.setUseSignature(false);
System.assertEquals(true, email.getBccSender());
System.assertEquals('High', email.getEmailPriority());
System.assertEquals('reply@cb193.example', email.getReplyTo());
System.assertEquals(false, email.getSaveAsActivity());
System.assertEquals('CB193 Sender', email.getSenderDisplayName());
System.assertEquals('CB193 Subject', email.getSubject());
System.assertEquals(false, email.getUseSignature());

// UserProvisioning: two process handlers and the three lowercase logging
// overloads. The abstract plugin shape is covered by the dedicated tail
// evidence fixture; runtime construction is intentionally not attempted.
UserProvisioning.ProvisioningProcessHandlerInput input = new UserProvisioning.ProvisioningProcessHandlerInput('upr', '005000000000001', 'named', 'filter', 'offset');
System.assert(new UserProvisioning.UserProvisioningProcessHandler().invoke(input) != null);
System.assert(new UserProvisioning.DummyConnectorApexHandler().invoke(input) != null);
userprovisioning.UserProvisioningLog.log('0PR-cb193', 'Created');
userprovisioning.UserProvisioningLog.log('0PR-cb193', 'Created', 'detail');
userprovisioning.UserProvisioningLog.log('0PR-cb193', 'Created', 'detail', 'field', 'value');
