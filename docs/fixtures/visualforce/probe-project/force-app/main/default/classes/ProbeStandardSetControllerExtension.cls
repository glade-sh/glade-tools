public with sharing class ProbeStandardSetControllerExtension {
    private ApexPages.StandardSetController controller;

    public ProbeStandardSetControllerExtension(ApexPages.StandardSetController controller) {
        this.controller = controller;
        this.controller.setPageSize(5);
    }

    public Integer getResultSize() {
        return controller.getResultSize();
    }

    public Boolean getHasNext() {
        return controller.getHasNext();
    }

    public Boolean getHasPrevious() {
        return controller.getHasPrevious();
    }

    public PageReference nextPage() {
        controller.next();
        return null;
    }

    public PageReference previousPage() {
        controller.previous();
        return null;
    }
}
