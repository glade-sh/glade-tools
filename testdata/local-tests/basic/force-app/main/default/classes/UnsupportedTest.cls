@isTest
private class UnsupportedTest {
  @isTest static void unsupported() {
    ConnectApi.ChatterUsers.getUserProfile(null);
  }
}
