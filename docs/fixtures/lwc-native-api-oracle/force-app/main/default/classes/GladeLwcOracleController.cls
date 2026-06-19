public with sharing class GladeLwcOracleController {
  @AuraEnabled(cacheable=true)
  public static String ping() {
    return 'ok';
  }

  @AuraEnabled(continuation=true cacheable=true)
  public static String continuationPing() {
    return 'ok';
  }
}
