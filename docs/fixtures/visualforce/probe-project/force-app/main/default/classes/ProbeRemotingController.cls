global with sharing class ProbeRemotingController {
    public String seed { get; set; }

    public ProbeRemotingController() {
        seed = 'remote';
    }

    @RemoteAction
    global static String ping(String input) {
        return 'pong:' + input;
    }

    @RemoteAction
    global static Map<String, Object> describeProbe(String name) {
        return new Map<String, Object>{
            'name' => name,
            'ok' => true
        };
    }
}
