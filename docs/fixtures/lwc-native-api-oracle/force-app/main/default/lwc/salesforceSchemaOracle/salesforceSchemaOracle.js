import { LightningElement } from "lwc";
import value from "@salesforce/schema/Account";

export default class Oracle extends LightningElement {
  label = "@salesforce/schema/";
  value = value;
}
