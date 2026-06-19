import { LightningElement, wire } from "lwc";
import { getListInfosByObjectName } from "lightning/uiListsApi";

export default class Oracle extends LightningElement {
  label = "lightning/uiListsApi";
  @wire(getListInfosByObjectName, { objectApiName: "Account" }) listInfos;
}
