import { LightningElement } from "lwc";
import value from "@salesforce/user/isGuest";

export default class Oracle extends LightningElement {
  label = "@salesforce/user/isGuest";
  value = value;
}
