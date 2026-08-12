@isTest
private class UnsupportedTest {
  @isTest static void unsupported() {
    PageReference page = new PageReference('/apex/Test');
    page.getContent();
  }
}
