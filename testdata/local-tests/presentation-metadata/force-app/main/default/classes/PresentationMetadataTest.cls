@isTest
private class PresentationMetadataTest {
  @isTest static void describesLocalTabs() {
    List<Object> tabSets = Schema.describeTabs();
    System.assertEquals(13, tabSets.size());
    Object tabSet;
    for (Object candidate : tabSets) {
      if (candidate.getLabel() == 'All Tabs') {
        tabSet = candidate;
      }
    }
    System.assertNotEquals(null, tabSet);
    System.assertEquals('All Tabs', tabSet.getLabel());
    List<Object> tabs = tabSet.getTabs();
    Boolean foundWidgetTab = false;
    for (Object tab : tabs) {
      if (tab.getSObjectName() == 'Widget__c') {
        System.assertEquals('Widget__c', tab.getName());
        System.assertEquals('Widgets', tab.getLabel());
        System.assert(tab.isCustom());
        foundWidgetTab = true;
      }
    }
    System.assert(foundWidgetTab);
  }
}
