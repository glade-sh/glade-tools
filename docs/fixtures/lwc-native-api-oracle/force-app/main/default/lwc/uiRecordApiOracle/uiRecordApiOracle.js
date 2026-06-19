import { LightningElement, wire } from "lwc";
import { getRecord, getFieldValue } from "lightning/uiRecordApi";

export default class Oracle extends LightningElement {
  label = "lightning/uiRecordApi";
  recordId = "001000000000001AAA";
  @wire(getRecord, { recordId: "$recordId", fields: ["Account.Name"] }) record;
  get value() {
    return getFieldValue(this.record?.data, "Account.Name") || "not loaded";
  }
}
