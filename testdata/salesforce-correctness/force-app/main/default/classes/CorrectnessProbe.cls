/**
 * Helper probe for lifecycle correctness tests.
 * Provides nested async types, callout mocks, and shared state
 * so each of the twelve test methods can verify one exact lifecycle case.
 */
public class CorrectnessProbe {

    // ── shared mutable state ────────────────────────────────────────────

    /** Reset between test methods; asserted from multiple methods. */
    public static Integer staticCounter = 0;

    // ── @future ─────────────────────────────────────────────────────────

    @future
    public static void insertFutureRecord() {
        insert new Account(Name = 'future-side-effect');
    }

    // ── Queueable ───────────────────────────────────────────────────────

    public class QueueableJob implements Queueable {
        public void execute(QueueableContext context) {
            insert new Account(Name = 'queueable-side-effect');
        }
    }

    // ── Schedulable ─────────────────────────────────────────────────────

    public class ScheduledJob implements Schedulable {
        public void execute(SchedulableContext sc) {
            insert new Account(Name = 'scheduled-side-effect');
        }
    }

    // ── HttpCalloutMock ─────────────────────────────────────────────────

    public class MockHttpResponse implements HttpCalloutMock {
        public HttpResponse respond(HttpRequest req) {
            System.assertEquals(
                'GET', req.getMethod(),
                'SF-LIFECYCLE-HTTP-CALLOUT-MOCK: request method'
            );
            System.assertEquals(
                'http://example.com', req.getEndpoint(),
                'SF-LIFECYCLE-HTTP-CALLOUT-MOCK: request endpoint'
            );
            HttpResponse res = new HttpResponse();
            res.setStatusCode(200);
            res.setBody('mock response');
            return res;
        }
    }

    // ── helper entry points ─────────────────────────────────────────────

    public static void enqueueFuture() {
        insertFutureRecord();
    }

    public static void enqueueQueueable() {
        System.enqueueJob(new QueueableJob());
    }

    public static void executeBatch() {
        Database.executeBatch(new CorrectnessProbeBatch(), 1);
    }

    public static String scheduleJob() {
        return System.schedule(
            'CorrectnessScheduled',
            '0 0 0 1 1 ? 2050',
            new ScheduledJob()
        );
    }

    public static String makeCallout() {
        HttpRequest req = new HttpRequest();
        req.setEndpoint('http://example.com');
        req.setMethod('GET');
        Http http = new Http();
        HttpResponse res = http.send(req);
        return res.getBody();
    }
}
