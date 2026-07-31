/**
 * Top-level batch implementation for lifecycle correctness tests.
 * Moved from CorrectnessProbe inner class so Salesforce compilation accepts
 * Database.Batchable<SObject> on a top-level Apex class.
 */
public class CorrectnessProbeBatch implements Database.Batchable<SObject> {
    public Database.QueryLocator start(Database.BatchableContext bc) {
        return Database.getQueryLocator([SELECT Id FROM Account LIMIT 1]);
    }
    public void execute(Database.BatchableContext bc, List<SObject> scope) {
        insert new Account(Name = 'batch-side-effect');
    }
    public void finish(Database.BatchableContext bc) {
    }
}
