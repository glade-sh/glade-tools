import { LightningElement } from "lwc";
import value from "@salesforce/i18n/locale";

export default class Oracle extends LightningElement {
  label = "@salesforce/i18n/";
  value = value;
}
