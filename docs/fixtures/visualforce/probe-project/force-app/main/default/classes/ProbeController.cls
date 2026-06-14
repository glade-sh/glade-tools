public with sharing class ProbeController {
    public String message { get; set; }
    public String country { get; set; }
    public String uploadName { get; set; }
    public Blob uploadBody { get; set; }

    public ProbeController() {
        message = 'probe ready';
        country = 'US';
    }

    public List<Account> getAccounts() {
        return new List<Account>{
            new Account(Name = 'Acme Probe'),
            new Account(Name = 'Birch Probe'),
            new Account(Name = 'Cedar Probe')
        };
    }

    public List<String> getRows() {
        return new List<String>{'alpha', 'bravo', 'charlie'};
    }

    public List<SelectOption> getCountryOptions() {
        return new List<SelectOption>{
            new SelectOption('US', 'United States'),
            new SelectOption('CA', 'Canada'),
            new SelectOption('MX', 'Mexico')
        };
    }

    public Integer getCount() {
        return getRows().size();
    }

    public Boolean getReady() {
        return true;
    }

    public PageReference step() {
        message = 'stepped';
        return null;
    }

    public PageReference save() {
        ApexPages.addMessage(new ApexPages.Message(ApexPages.Severity.CONFIRM, 'Probe saved'));
        return null;
    }

    public PageReference failSoft() {
        ApexPages.addMessage(new ApexPages.Message(ApexPages.Severity.ERROR, 'Probe error'));
        return null;
    }

    public PageReference getFlowFinish() {
        return new PageReference('/apex/ProbeLifecycleBasic');
    }
}
