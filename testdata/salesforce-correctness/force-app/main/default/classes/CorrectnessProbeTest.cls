/**
 * Twelve lifecycle correctness probes.
 * Each @IsTest method maps to exactly one required case ID.
 * The ID appears verbatim in every assertion message so an oracle
 * can report an exact case instead of a prose finding.
 */
@IsTest(SeeAllData=false)
private class CorrectnessProbeTest {

    // ── shared setup ────────────────────────────────────────────────────

    @TestSetup
    static void createSetupData() {
        insert new Account(Name = 'Setup Account');
    }

    // 1 ──────────────────────────────────────────────────────────────────

    @IsTest
    static void sfLifecycleDiscovery() {
        // SF-LIFECYCLE-DISCOVERY: The Salesforce test runner discovered and
        // executed this method. That execution is the discovery observation.
        System.assert(true, 'SF-LIFECYCLE-DISCOVERY: method executed by test runner');
    }

    // 2 ──────────────────────────────────────────────────────────────────

    @IsTest
    static void sfLifecycleTestSetupExecution() {
        System.assertEquals(
            0, CorrectnessProbe.staticCounter,
            'SF-LIFECYCLE-TEST-SETUP-EXECUTION: static reset'
        );

        List<Account> accts = [
            SELECT Id, Name FROM Account WHERE Name = 'Setup Account'
        ];
        System.assertEquals(
            1, accts.size(),
            'SF-LIFECYCLE-TEST-SETUP-EXECUTION: setup ran and created data'
        );
    }

    // 3 ──────────────────────────────────────────────────────────────────

    @IsTest
    static void sfLifecycleTestSetupCopy() {
        System.assertEquals(
            0, CorrectnessProbe.staticCounter,
            'SF-LIFECYCLE-TEST-SETUP-COPY: static reset'
        );

        List<Account> accts = [
            SELECT Id, Name FROM Account WHERE Name = 'Setup Account'
        ];
        System.assertEquals(
            1, accts.size(),
            'SF-LIFECYCLE-TEST-SETUP-COPY: begins with only copied setup'
        );

        accts[0].Name = 'Modified in copy';
        update accts[0];

        List<Account> modified = [
            SELECT Name FROM Account WHERE Name = 'Modified in copy'
        ];
        System.assertEquals(
            1, modified.size(),
            'SF-LIFECYCLE-TEST-SETUP-COPY: modification took in local copy'
        );
    }

    // 4 ──────────────────────────────────────────────────────────────────

    @IsTest
    static void sfLifecycleMethodRollback() {
        System.assertEquals(
            0, CorrectnessProbe.staticCounter,
            'SF-LIFECYCLE-METHOD-ROLLBACK: static reset'
        );

        Integer before = [SELECT COUNT() FROM Account];
        System.assertEquals(
            1, before,
            'SF-LIFECYCLE-METHOD-ROLLBACK: begins with only setup data'
        );

        insert new Account(Name = 'Method-local Account');

        Integer after = [SELECT COUNT() FROM Account];
        System.assertEquals(
            2, after,
            'SF-LIFECYCLE-METHOD-ROLLBACK: method-local insert visible'
        );
        // platform rolls back the method-local insert after this method
    }

    // 5 ──────────────────────────────────────────────────────────────────

    @IsTest
    static void sfLifecycleStaticReset() {
        System.assertEquals(
            0, CorrectnessProbe.staticCounter,
            'SF-LIFECYCLE-STATIC-RESET: initial value at method start'
        );

        CorrectnessProbe.staticCounter = 42;

        System.assertEquals(
            42, CorrectnessProbe.staticCounter,
            'SF-LIFECYCLE-STATIC-RESET: value changed within method'
        );
    }

    // 6 ──────────────────────────────────────────────────────────────────

    @IsTest
    static void sfLifecycleStartTestLimits() {
        // Consume at least one query
        Integer unused = [SELECT COUNT() FROM Account];
        Integer queriesBefore = Limits.getQueries();
        System.assert(
            queriesBefore > 0,
            'SF-LIFECYCLE-START-TEST-LIMITS: consumed queries before boundary'
        );

        Test.startTest();
        Integer queriesAfter = Limits.getQueries();
        Test.stopTest();

        System.assert(
            queriesAfter < queriesBefore,
            'SF-LIFECYCLE-START-TEST-LIMITS: query count reset by startTest'
        );
    }

    // 7 ──────────────────────────────────────────────────────────────────

    @IsTest
    static void sfLifecycleStopTestFutureQueueable() {
        // No side-effect records exist yet
        Integer beforeFuture = [
            SELECT COUNT() FROM Account WHERE Name = 'future-side-effect'
        ];
        Integer beforeQueueable = [
            SELECT COUNT() FROM Account WHERE Name = 'queueable-side-effect'
        ];
        System.assertEquals(
            0, beforeFuture,
            'SF-LIFECYCLE-STOP-TEST-FUTURE-QUEUEABLE: no future record before'
        );
        System.assertEquals(
            0, beforeQueueable,
            'SF-LIFECYCLE-STOP-TEST-FUTURE-QUEUEABLE: no queueable record before'
        );

        Test.startTest();
        CorrectnessProbe.enqueueFuture();
        CorrectnessProbe.enqueueQueueable();
        Test.stopTest();

        Integer afterFuture = [
            SELECT COUNT() FROM Account WHERE Name = 'future-side-effect'
        ];
        Integer afterQueueable = [
            SELECT COUNT() FROM Account WHERE Name = 'queueable-side-effect'
        ];
        System.assertEquals(
            1, afterFuture,
            'SF-LIFECYCLE-STOP-TEST-FUTURE-QUEUEABLE: future side effect written'
        );
        System.assertEquals(
            1, afterQueueable,
            'SF-LIFECYCLE-STOP-TEST-FUTURE-QUEUEABLE: queueable side effect written'
        );
    }

    // 8 ──────────────────────────────────────────────────────────────────

    @IsTest
    static void sfLifecycleStopTestBatchScheduled() {
        // ensure batch has a record to process
        insert new Account(Name = 'Batch target');

        Integer beforeBatch = [
            SELECT COUNT() FROM Account WHERE Name = 'batch-side-effect'
        ];
        Integer beforeScheduled = [
            SELECT COUNT() FROM Account WHERE Name = 'scheduled-side-effect'
        ];
        System.assertEquals(
            0, beforeBatch,
            'SF-LIFECYCLE-STOP-TEST-BATCH-SCHEDULED: no batch record before'
        );
        System.assertEquals(
            0, beforeScheduled,
            'SF-LIFECYCLE-STOP-TEST-BATCH-SCHEDULED: no scheduled record before'
        );

        Test.startTest();
        CorrectnessProbe.executeBatch();
        String jobId = CorrectnessProbe.scheduleJob();
        System.assertNotEquals(
            null, jobId,
            'SF-LIFECYCLE-STOP-TEST-BATCH-SCHEDULED: scheduled job queued'
        );
        Test.stopTest();

        Integer afterBatch = [
            SELECT COUNT() FROM Account WHERE Name = 'batch-side-effect'
        ];
        Integer afterScheduled = [
            SELECT COUNT() FROM Account WHERE Name = 'scheduled-side-effect'
        ];
        System.assertEquals(
            1, afterBatch,
            'SF-LIFECYCLE-STOP-TEST-BATCH-SCHEDULED: batch side effect written'
        );
        System.assertEquals(
            0, afterScheduled,
            'SF-LIFECYCLE-STOP-TEST-BATCH-SCHEDULED: scheduled execute does not run inside stopTest drain'
        );

        List<CronTrigger> jobs = [
            SELECT Id FROM CronTrigger
            WHERE CronJobDetail.Name = 'CorrectnessScheduled'
        ];
        System.assertEquals(
            1, jobs.size(),
            'SF-LIFECYCLE-STOP-TEST-BATCH-SCHEDULED: scheduled job durable in CronTrigger'
        );
    }

    // 9 ──────────────────────────────────────────────────────────────────

    @IsTest
    static void sfLifecycleHttpCalloutMock() {
        Test.setMock(
            HttpCalloutMock.class,
            new CorrectnessProbe.MockHttpResponse()
        );

        Test.startTest();
        String body = CorrectnessProbe.makeCallout();
        Test.stopTest();

        System.assertEquals(
            'mock response', body,
            'SF-LIFECYCLE-HTTP-CALLOUT-MOCK: mock intercepted the callout'
        );
    }

    // 10 ─────────────────────────────────────────────────────────────────

    @IsTest
    static void sfLifecycleDmlAllOrNone() {
        Account valid   = new Account(Name = 'Valid');
        Account invalid = new Account();               // missing required Name

        List<Database.SaveResult> results =
            Database.insert(new List<Account>{ valid, invalid }, false);

        System.assertEquals(
            2, results.size(),
            'SF-LIFECYCLE-DML-ALL-OR-NONE: two results returned'
        );

        System.assert(
            results[0].isSuccess(),
            'SF-LIFECYCLE-DML-ALL-OR-NONE: first insert succeeded'
        );
        System.assertNotEquals(
            null, results[0].getId(),
            'SF-LIFECYCLE-DML-ALL-OR-NONE: successful result has ID'
        );

        System.assert(
            !results[1].isSuccess(),
            'SF-LIFECYCLE-DML-ALL-OR-NONE: second insert failed'
        );
        System.assertEquals(
            null, results[1].getId(),
            'SF-LIFECYCLE-DML-ALL-OR-NONE: failed result has no ID'
        );
        System.assert(
            results[1].getErrors().size() > 0,
            'SF-LIFECYCLE-DML-ALL-OR-NONE: error details present on failure'
        );
        System.assertEquals(
            System.StatusCode.REQUIRED_FIELD_MISSING,
            results[1].getErrors()[0].getStatusCode(),
            'SF-LIFECYCLE-DML-ALL-OR-NONE: expected REQUIRED_FIELD_MISSING status'
        );
    }

    // 11 ─────────────────────────────────────────────────────────────────

    @IsTest
    static void sfLifecycleTransactionVisibility() {
        Account a = new Account(Name = 'Visibility test');
        insert a;

        List<Account> found = [
            SELECT Id, Name FROM Account WHERE Name = 'Visibility test'
        ];
        System.assertEquals(
            1, found.size(),
            'SF-LIFECYCLE-TRANSACTION-VISIBILITY: record visible same-tx'
        );

        Savepoint sp = Database.setSavepoint();
        insert new Account(Name = 'Rollback target');

        Integer afterInsert = [
            SELECT COUNT() FROM Account WHERE Name = 'Rollback target'
        ];
        System.assertEquals(
            1, afterInsert,
            'SF-LIFECYCLE-TRANSACTION-VISIBILITY: extra record visible before rollback'
        );

        Database.rollback(sp);

        Integer afterRollback = [
            SELECT COUNT() FROM Account WHERE Name = 'Rollback target'
        ];
        System.assertEquals(
            0, afterRollback,
            'SF-LIFECYCLE-TRANSACTION-VISIBILITY: extra record gone after rollback'
        );
    }

    // 12 ─────────────────────────────────────────────────────────────────

    @IsTest
    static void sfLifecycleRunAs() {
        // Retrieve profile from org metadata — no hard-coded ID.
        Profile p = [SELECT Id FROM Profile LIMIT 1];

        String uniqueName =
            'correctnessrunas' + DateTime.now().getTime() + '@test.org';

        User testUser = new User(
            Alias              = 'crns',
            CommunityNickname  = uniqueName.left(40),
            Email              = uniqueName,
            EmailEncodingKey   = 'UTF-8',
            LastName           = 'CorrectnessRunAs',
            LanguageLocaleKey  = 'en_US',
            LocaleSidKey       = 'en_US',
            ProfileId          = p.Id,
            TimeZoneSidKey     = 'America/Los_Angeles',
            Username           = uniqueName
        );
        insert testUser;

        Id originalUserId = UserInfo.getUserId();

        System.runAs(testUser) {
            Id runAsUserId = UserInfo.getUserId();
            System.assertEquals(
                testUser.Id, runAsUserId,
                'SF-LIFECYCLE-RUN-AS: running as created user'
            );
        }

        Id afterUserId = UserInfo.getUserId();
        System.assertEquals(
            originalUserId, afterUserId,
            'SF-LIFECYCLE-RUN-AS: context restored after runAs block'
        );
    }
}
